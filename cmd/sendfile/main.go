package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sendfile/internal/config"
	"sendfile/internal/discovery"
	"sendfile/internal/server"
	"sendfile/internal/transfer"
	"sendfile/internal/ui"
	"syscall"
	"time"
)

func main() {
	portFlag := flag.Int("port", 53318, "HTTP port to bind (default 53318)")
	nameFlag := flag.String("name", "", "Friendly device name (defaults to hostname)")
	dirFlag := flag.String("dir", "", "Directory to save incoming files (defaults to Downloads/SendFile)")
	autoAcceptFlag := flag.Bool("auto-accept", false, "Automatically accept incoming file transfers without asking")
	noBrowserFlag := flag.Bool("no-browser", false, "Do not automatically launch web browser on startup")
	flag.Parse()

	// 1. Initialize Configuration
	cfg := config.NewDefaultConfig(*portFlag, *autoAcceptFlag, *nameFlag, *dirFlag)

	// 2. Initialize Peer Table & Discovery
	var sseBroker *server.SSEBroker
	peerTable := discovery.NewPeerTable(func(peers []*discovery.Peer) {
		if sseBroker != nil {
			sseBroker.Broadcast("peer_list", peers)
		}
	})

	discoverySvc := discovery.NewDiscoveryService(cfg, peerTable)

	// 3. Initialize Transfer Manager
	transferMgr := transfer.NewTransferManager(cfg, func(session *transfer.TransferSession) {
		if sseBroker != nil {
			sseBroker.Broadcast("transfer_update", session)
		}
	})

	// 4. Initialize HTTP Server with Embedded UI
	staticFS := ui.GetFS()
	srv := server.NewServer(cfg, peerTable, discoverySvc, transferMgr, staticFS)
	sseBroker = srv.GetSSEBroker()

	// 5. Start Server and Discovery
	if err := srv.Start(); err != nil {
		log.Fatalf("Failed to start HTTP server: %v", err)
	}

	if err := discoverySvc.Start(); err != nil {
		log.Printf("Warning: Discovery service issue: %v", err)
	}

	printBanner(cfg)

	// 6. Open browser on launch
	localURL := fmt.Sprintf("http://localhost:%d", cfg.HTTPPort)
	if !*noBrowserFlag {
		go func() {
			time.Sleep(400 * time.Millisecond)
			openBrowser(localURL)
		}()
	}

	// 7. Wait for Interrupt / Termination
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	fmt.Println("\n[SendFile] Shutting down gracefully...")
	discoverySvc.Stop()
	srv.Stop()
	fmt.Println("[SendFile] Goodbye!")
}

func printBanner(cfg *config.Config) {
	fmt.Println("================================================================")
	fmt.Println("   ____                 _______ _ _        ")
	fmt.Println("  / ___|  ___ _ __   __| |  ___(_) | ___   ")
	fmt.Println("  \\___ \\ / _ \\ '_ \\ / _` | |_  | | |/ _ \\  ")
	fmt.Println("   ___) |  __/ | | | (_| |  _| | | |  __/  ")
	fmt.Println("  |____/ \\___|_| |_|\\__,_|_|   |_|_|\\___|  ")
	fmt.Println("================================================================")
	fmt.Printf(" Device Name  : %s (%s / %s)\n", cfg.GetDeviceName(), cfg.DeviceType, cfg.OS)
	fmt.Printf(" Web UI (PC)  : http://localhost:%d\n", cfg.HTTPPort)
	fmt.Printf(" Wi-Fi URL    : http://%s:%d\n", cfg.PrimaryIP, cfg.HTTPPort)
	fmt.Printf(" Save Folder  : %s\n", cfg.GetSaveDir())
	fmt.Printf(" Auto-Accept  : %v\n", cfg.IsAutoAccept())
	fmt.Println("----------------------------------------------------------------")
	fmt.Println(" Point phone or other devices to the Wi-Fi URL above to transfer!")
	fmt.Println(" Press Ctrl+C to stop.")
	fmt.Println("================================================================")
}

func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}

	_ = exec.Command(cmd, args...).Start()
}
