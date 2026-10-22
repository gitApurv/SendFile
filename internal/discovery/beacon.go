package discovery

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sendfile/internal/config"
	"sync"
	"time"
)

const (
	MulticastGroup = "239.255.77.77:53317"
	BroadcastPort  = 53317
	ProtocolVer    = "1.0.0"
)

type BeaconMessage struct {
	Action     string `json:"action"` // "beacon", "leave", "ping", "pong"
	ID         string `json:"id"`
	Name       string `json:"name"`
	DeviceType string `json:"device_type"`
	OS         string `json:"os"`
	HTTPPort   int    `json:"http_port"`
	Version    string `json:"version"`
}

type DiscoveryService struct {
	cfg            *config.Config
	peers          *PeerTable
	stopCh         chan struct{}
	wg             sync.WaitGroup
	conn           *net.UDPConn
	broadcastAddrs []*net.UDPAddr
}

func NewDiscoveryService(cfg *config.Config, peers *PeerTable) *DiscoveryService {
	return &DiscoveryService{
		cfg:    cfg,
		peers:  peers,
		stopCh: make(chan struct{}),
	}
}

func (ds *DiscoveryService) Start() error {
	ds.calculateBroadcastAddresses()

	mAddr, err := net.ResolveUDPAddr("udp4", MulticastGroup)
	if err != nil {
		return fmt.Errorf("failed to resolve multicast address: %w", err)
	}

	// Try to listen on multicast group (allows multiple listeners on the same machine)
	conn, err := net.ListenMulticastUDP("udp4", nil, mAddr)
	if err != nil {
		// Fallback to regular UDP listener on BroadcastPort
		log.Printf("[Discovery] Multicast listen failed (%v), falling back to standard UDP", err)
		bAddr, bErr := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", BroadcastPort))
		if bErr != nil {
			return fmt.Errorf("failed to resolve udp port: %w", bErr)
		}
		conn, err = net.ListenUDP("udp4", bAddr)
		if err != nil {
			log.Printf("[Discovery] Standard UDP listen also failed (%v). Continuing in client-only broadcast mode.", err)
		}
	}

	ds.conn = conn

	if ds.conn != nil {
		ds.wg.Add(1)
		go ds.listenLoop()
	}

	ds.wg.Add(1)
	go ds.broadcastLoop()

	// Initial announce
	ds.BroadcastBeacon("beacon")

	return nil
}

func (ds *DiscoveryService) Stop() {
	close(ds.stopCh)
	// Announce departure
	ds.BroadcastBeacon("leave")
	if ds.conn != nil {
		_ = ds.conn.Close()
	}
	ds.wg.Wait()
}

func (ds *DiscoveryService) BroadcastBeacon(action string) {
	msg := BeaconMessage{
		Action:     action,
		ID:         ds.cfg.NodeID,
		Name:       ds.cfg.GetDeviceName(),
		DeviceType: ds.cfg.DeviceType,
		OS:         ds.cfg.OS,
		HTTPPort:   ds.cfg.HTTPPort,
		Version:    ProtocolVer,
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}

	// Send to multicast group
	mAddr, err := net.ResolveUDPAddr("udp4", MulticastGroup)
	if err == nil {
		sendConn, sErr := net.DialUDP("udp4", nil, mAddr)
		if sErr == nil {
			_, _ = sendConn.Write(payload)
			_ = sendConn.Close()
		}
	}

	// Send to interface broadcast addresses and global 255.255.255.255
	for _, addr := range ds.broadcastAddrs {
		sendConn, sErr := net.DialUDP("udp4", nil, addr)
		if sErr == nil {
			_, _ = sendConn.Write(payload)
			_ = sendConn.Close()
		}
	}
}

func (ds *DiscoveryService) broadcastLoop() {
	defer ds.wg.Done()
	ticker := time.NewTicker(2500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ds.stopCh:
			return
		case <-ticker.C:
			ds.BroadcastBeacon("beacon")
		}
	}
}

func (ds *DiscoveryService) listenLoop() {
	defer ds.wg.Done()
	buf := make([]byte, 2048)

	for {
		n, remoteAddr, err := ds.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ds.stopCh:
				return
			default:
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}

		var msg BeaconMessage
		if err := json.Unmarshal(buf[:n], &msg); err != nil {
			continue
		}

		// Ignore own messages
		if msg.ID == ds.cfg.NodeID {
			continue
		}

		senderIP := remoteAddr.IP.String()

		switch msg.Action {
		case "leave":
			ds.peers.Remove(msg.ID)
		case "ping":
			// Reply immediately with pong
			ds.BroadcastBeacon("pong")
			fallthrough
		case "beacon", "pong":
			peer := &Peer{
				ID:         msg.ID,
				Name:       msg.Name,
				DeviceType: msg.DeviceType,
				OS:         msg.OS,
				IP:         senderIP,
				Port:       msg.HTTPPort,
				Version:    msg.Version,
			}
			ds.peers.Upsert(peer)
		}
	}
}

func (ds *DiscoveryService) calculateBroadcastAddresses() {
	var addrs []*net.UDPAddr

	// Global broadcast
	gAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("255.255.255.255:%d", BroadcastPort))
	if err == nil {
		addrs = append(addrs, gAddr)
	}

	// Subnet broadcasts
	ifaces, err := net.Interfaces()
	if err != nil {
		ds.broadcastAddrs = addrs
		return
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		ifAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range ifAddrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok || ipNet.IP.IsLoopback() {
				continue
			}
			ipv4 := ipNet.IP.To4()
			if ipv4 == nil {
				continue
			}

			// Compute subnet broadcast: ip | ~mask
			mask := ipNet.Mask
			if len(mask) == 4 {
				bcast := net.IPv4(
					ipv4[0]|^mask[0],
					ipv4[1]|^mask[1],
					ipv4[2]|^mask[2],
					ipv4[3]|^mask[3],
				)
				bAddr, bErr := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", bcast.String(), BroadcastPort))
				if bErr == nil {
					addrs = append(addrs, bAddr)
				}
			}
		}
	}

	ds.broadcastAddrs = addrs
}
