# SendFile 🚀
> High-performance, zero-config local Wi-Fi file transfer application built in Go.

Transfer files, folders, photos, videos, and documents between any devices connected to the same Wi-Fi network without cloud storage, file size limits, or internet bandwidth.

---

## Features

- **⚡ Zero Configuration Discovery:** Uses UDP Multicast (`239.255.77.77:53317`) and Subnet Broadcasts to discover nearby devices running SendFile on the same Wi-Fi instantly.
- **📱 Universal Cross-Platform & Phone Support:**
  - Dedicated executable for Windows, macOS, and Linux.
  - **Zero-Install Phone Mode:** Point any iPhone or Android camera at the in-app QR code to send and receive files directly in the mobile browser—no mobile app installation required!
- **🔒 Secure Transfer Handshake:** Receivers are prompted with an Accept/Decline modal showing the file list and total size before any bytes are accepted.
- **🚀 High-Throughput Chunked Streaming:** Directly streams from network to disk with constant memory footprint (~few KB RAM) regardless of file size (supports multi-gigabyte files).
- **📊 Real-Time Telemetry:** Live transfer speed (MB/s), percentage progress bar, and estimated time remaining (ETA) via Server-Sent Events (SSE).
- **📦 Single Self-Contained Binary:** All UI assets, icons, styles, and logic are embedded directly into the binary via Go's `//go:embed`.

---

## Quick Start

### 1. Run on Windows
Simply double-click `sendfile.exe` or run from terminal:
```powershell
.\sendfile.exe
```
This automatically launches your default web browser to:
`http://localhost:53318`

### 2. Connect from Phone (iPhone / Android)
1. In the desktop app, click the **QR Code** icon in the top navigation bar.
2. Scan the QR code using your phone's camera (or open `http://<YOUR_PC_IP>:53318` in Safari/Chrome).
3. Tap **Browse Files**, select photos, videos, or documents, and tap **Send Files**!

### 3. Connect from Another Computer
Run `sendfile` on the other computer. Both machines will detect each other automatically in the **Nearby Wi-Fi Devices** panel!

---

## Command-Line Options

```
Usage of sendfile:
  -port int
        HTTP port to bind (default 53318)
  -name string
        Friendly device name (defaults to system hostname)
  -dir string
        Directory to save incoming files (defaults to Downloads/SendFile)
  -auto-accept
        Automatically accept incoming file transfers without asking
  -no-browser
        Do not automatically launch web browser on startup
```

### Examples

Run on a custom port with a custom device name:
```powershell
.\sendfile.exe -name "LivingRoom-PC" -port 8080
```

Run in headless / auto-accept mode:
```powershell
.\sendfile.exe -no-browser -auto-accept -dir "D:\SharedDownloads"
```

---

## Building from Source

Requirements: Go 1.20+

```powershell
# Build executable for Windows
go build -o sendfile.exe ./cmd/sendfile

# Cross-compile for macOS (Apple Silicon / Intel)
GOOS=darwin GOARCH=arm64 go build -o sendfile-macos-arm64 ./cmd/sendfile
GOOS=darwin GOARCH=amd64 go build -o sendfile-macos-amd64 ./cmd/sendfile

# Cross-compile for Linux
GOOS=linux GOARCH=amd64 go build -o sendfile-linux ./cmd/sendfile
```

---

## Architecture & Technical Deep Dive

For an in-depth breakdown of networking daemons, streaming engines, destination-only authorization, and protocol schemas, see **[`CONTEXT.md`](file:///c:/Users/apurv/OneDrive/Desktop/Downloads/SendFile/CONTEXT.md)**.

```
SendFile/
├── cmd/
│   └── sendfile/
│       └── main.go          # Application startup, flags, lifecycle
├── internal/
│   ├── config/              # Device identity, save path, LAN IP detection
│   ├── discovery/           # UDP multicast & broadcast beaconing
│   ├── server/              # HTTP REST API, SSE streaming, file handlers
│   ├── transfer/            # Chunked streaming engine & telemetry
│   └── ui/                  # Embedded responsive web interface
│       └── static/          # HTML5, CSS, and JS
├── go.mod
├── README.md                # Quick start and CLI options
├── CONTEXT.md               # Complete process & protocol deep dive
└── sendfile.exe             # Self-contained executable
```
