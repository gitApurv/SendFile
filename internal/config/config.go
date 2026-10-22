package config

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type Config struct {
	mu         sync.RWMutex
	NodeID     string   `json:"node_id"`
	DeviceName string   `json:"device_name"`
	DeviceType string   `json:"device_type"`
	OS         string   `json:"os"`
	HTTPPort   int      `json:"http_port"`
	UDPPort    int      `json:"udp_port"`
	SaveDir    string   `json:"save_dir"`
	AutoAccept bool     `json:"auto_accept"`
	LocalIPs   []string `json:"local_ips"`
	PrimaryIP  string   `json:"primary_ip"`
}

func NewDefaultConfig(port int, autoAccept bool, customName, customDir string) *Config {
	nodeID := generateRandomID(8)
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "Device-" + nodeID[:4]
	}

	deviceName := hostname
	if customName != "" {
		deviceName = customName
	}

	saveDir := customDir
	if saveDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			saveDir = filepath.Join(home, "Downloads", "SendFile")
		} else {
			saveDir = "received_files"
		}
	}

	// Ensure save directory exists
	_ = os.MkdirAll(saveDir, 0755)

	devType := "desktop"
	if runtime.GOOS == "android" || runtime.GOOS == "ios" {
		devType = "mobile"
	}

	ips, primary := GetLocalIPs()

	return &Config{
		NodeID:     nodeID,
		DeviceName: deviceName,
		DeviceType: devType,
		OS:         runtime.GOOS,
		HTTPPort:   port,
		UDPPort:    53317,
		SaveDir:    saveDir,
		AutoAccept: autoAccept,
		LocalIPs:   ips,
		PrimaryIP:  primary,
	}
}

func (c *Config) GetDeviceName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.DeviceName
}

func (c *Config) SetDeviceName(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if name != "" {
		c.DeviceName = name
	}
}

func (c *Config) GetSaveDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.SaveDir
}

func (c *Config) SetSaveDir(dir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	c.SaveDir = dir
	return nil
}

func (c *Config) IsAutoAccept() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.AutoAccept
}

func (c *Config) SetAutoAccept(val bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.AutoAccept = val
}

func (c *Config) RefreshIPs() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LocalIPs, c.PrimaryIP = GetLocalIPs()
}

func GetLocalIPs() ([]string, string) {
	var ips []string
	var primary string
	var wifiIP string
	var ethernetIP string

	ifaces, err := net.Interfaces()
	if err != nil {
		return []string{"127.0.0.1"}, "127.0.0.1"
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		ifName := strings.ToLower(iface.Name)
		isVirtual := strings.Contains(ifName, "virtual") ||
			strings.Contains(ifName, "vethernet") ||
			strings.Contains(ifName, "vbox") ||
			strings.Contains(ifName, "vmware") ||
			strings.Contains(ifName, "*") // Windows virtual Wi-Fi adapters like "Local Area Connection* 2"

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.IsLoopback() {
				continue
			}
			ipv4 := ipNet.IP.To4()
			if ipv4 == nil {
				continue
			}

			ipStr := ipv4.String()
			// Filter out APIPA (169.254.x.x)
			if strings.HasPrefix(ipStr, "169.254.") {
				continue
			}

			ips = append(ips, ipStr)

			// Deprioritize default Windows mobile hotspot IP (192.168.137.1)
			if strings.HasPrefix(ipStr, "192.168.137.") {
				continue
			}

			if isVirtual {
				continue
			}

			// Prioritize Wi-Fi interface
			if strings.Contains(ifName, "wi-fi") || strings.Contains(ifName, "wifi") || strings.Contains(ifName, "wlan") {
				wifiIP = ipStr
			} else if strings.Contains(ifName, "ethernet") || strings.Contains(ifName, "eth") || strings.Contains(ifName, "en") {
				if ethernetIP == "" {
					ethernetIP = ipStr
				}
			}
		}
	}

	// Select best primary IP
	if wifiIP != "" {
		primary = wifiIP
	} else if ethernetIP != "" {
		primary = ethernetIP
	} else {
		// Pick first private IP that is not 192.168.137.1
		for _, ip := range ips {
			if !strings.HasPrefix(ip, "192.168.137.") {
				primary = ip
				break
			}
		}
	}

	if primary == "" && len(ips) > 0 {
		primary = ips[0]
	}
	if primary == "" {
		primary = "127.0.0.1"
		ips = append(ips, primary)
	}

	return ips, primary
}

func generateRandomID(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "node1234"
	}
	return hex.EncodeToString(bytes)
}
