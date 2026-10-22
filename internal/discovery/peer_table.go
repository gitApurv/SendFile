package discovery

import (
	"sync"
	"time"
)

type Peer struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	DeviceType string    `json:"device_type"`
	OS         string    `json:"os"`
	IP         string    `json:"ip"`
	Port       int       `json:"port"`
	Version    string    `json:"version"`
	LastSeen   time.Time `json:"last_seen"`
	IsWeb      bool      `json:"is_web,omitempty"`
}

type PeerTable struct {
	mu               sync.RWMutex
	peers            map[string]*Peer
	onPeerListChange func([]*Peer)
}

func NewPeerTable(onChange func([]*Peer)) *PeerTable {
	pt := &PeerTable{
		peers:            make(map[string]*Peer),
		onPeerListChange: onChange,
	}

	go pt.cleanupLoop(7 * time.Second)
	return pt
}

func (pt *PeerTable) Upsert(p *Peer) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	p.LastSeen = time.Now()
	pt.peers[p.ID] = p

	if pt.onPeerListChange != nil {
		go pt.notify()
	}
}

func (pt *PeerTable) Remove(id string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if _, exists := pt.peers[id]; exists {
		delete(pt.peers, id)
		if pt.onPeerListChange != nil {
			go pt.notify()
		}
	}
}

func (pt *PeerTable) Get(id string) (*Peer, bool) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	p, exists := pt.peers[id]
	if !exists {
		return nil, false
	}
	cp := *p
	return &cp, true
}

func (pt *PeerTable) List() []*Peer {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	list := make([]*Peer, 0, len(pt.peers))
	for _, p := range pt.peers {
		cp := *p
		list = append(list, &cp)
	}
	return list
}

func (pt *PeerTable) notify() {
	list := pt.List()
	if pt.onPeerListChange != nil {
		pt.onPeerListChange(list)
	}
}

func (pt *PeerTable) cleanupLoop(timeout time.Duration) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		pt.mu.Lock()
		changed := false
		now := time.Now()

		for id, peer := range pt.peers {
			if now.Sub(peer.LastSeen) > timeout {
				delete(pt.peers, id)
				changed = true
			}
		}
		pt.mu.Unlock()

		if changed && pt.onPeerListChange != nil {
			pt.notify()
		}
	}
}
