package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sendfile/internal/config"
	"sendfile/internal/discovery"
	"sendfile/internal/transfer"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ServerHandlers struct {
	cfg                *config.Config
	peers              *discovery.PeerTable
	discovery          *discovery.DiscoveryService
	transfers          *transfer.TransferManager
	sse                *SSEBroker
	stagedMu           sync.RWMutex
	stagedWebDownloads map[string][]*transfer.LocalFileInfo
}

func NewServerHandlers(
	cfg *config.Config,
	peers *discovery.PeerTable,
	discovery *discovery.DiscoveryService,
	transfers *transfer.TransferManager,
	sse *SSEBroker,
) *ServerHandlers {
	return &ServerHandlers{
		cfg:                cfg,
		peers:              peers,
		discovery:          discovery,
		transfers:          transfers,
		sse:                sse,
		stagedWebDownloads: make(map[string][]*transfer.LocalFileInfo),
	}
}

// GET /api/info
func (h *ServerHandlers) HandleGetInfo(w http.ResponseWriter, r *http.Request) {
	h.cfg.RefreshIPs()
	writeJSON(w, http.StatusOK, h.cfg)
}

// POST /api/settings
func (h *ServerHandlers) HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceName *string `json:"device_name"`
		SaveDir    *string `json:"save_dir"`
		AutoAccept *bool   `json:"auto_accept"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid JSON")
		return
	}

	if req.DeviceName != nil && strings.TrimSpace(*req.DeviceName) != "" {
		h.cfg.SetDeviceName(strings.TrimSpace(*req.DeviceName))
		h.discovery.BroadcastBeacon("beacon")
	}

	if req.SaveDir != nil && strings.TrimSpace(*req.SaveDir) != "" {
		if err := h.cfg.SetSaveDir(strings.TrimSpace(*req.SaveDir)); err != nil {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Failed to set save dir: %v", err))
			return
		}
	}

	if req.AutoAccept != nil {
		h.cfg.SetAutoAccept(*req.AutoAccept)
	}

	writeJSON(w, http.StatusOK, h.cfg)
}

// GET /api/peers
func (h *ServerHandlers) HandleGetPeers(w http.ResponseWriter, r *http.Request) {
	peers := h.peers.List()
	writeJSON(w, http.StatusOK, peers)
}

// POST /api/peers/register-web
func (h *ServerHandlers) HandleRegisterWebPeer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID   string `json:"client_id"`
		Name       string `json:"name"`
		DeviceType string `json:"device_type"`
		OS         string `json:"os"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ClientID == "" {
		writeJSONError(w, http.StatusBadRequest, "Invalid web peer payload")
		return
	}

	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}

	name := req.Name
	if name == "" {
		name = "Phone (" + remoteIP + ")"
	}
	devType := req.DeviceType
	if devType == "" {
		devType = "mobile"
	}
	osName := req.OS
	if osName == "" {
		osName = "web"
	}

	h.peers.Upsert(&discovery.Peer{
		ID:         req.ClientID,
		Name:       name,
		DeviceType: devType,
		OS:         osName,
		IP:         remoteIP,
		Port:       h.cfg.HTTPPort,
		Version:    "web-1.0",
		IsWeb:      true,
	})

	writeJSON(w, http.StatusOK, map[string]bool{"registered": true})
}

// POST /api/peers/unregister-web
func (h *ServerHandlers) HandleUnregisterWebPeer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.ClientID != "" {
		h.peers.Remove(req.ClientID)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"unregistered": true})
}

// GET /api/download?transfer_id=...&file_index=...
func (h *ServerHandlers) HandleDownloadFile(w http.ResponseWriter, r *http.Request) {
	transferID := r.URL.Query().Get("transfer_id")
	fileIdxStr := r.URL.Query().Get("file_index")
	if transferID == "" || fileIdxStr == "" {
		http.Error(w, "Missing transfer_id or file_index", http.StatusBadRequest)
		return
	}

	idx, err := strconv.Atoi(fileIdxStr)
	if err != nil {
		http.Error(w, "Invalid file_index", http.StatusBadRequest)
		return
	}

	h.stagedMu.RLock()
	files, ok := h.stagedWebDownloads[transferID]
	h.stagedMu.RUnlock()

	if !ok || idx < 0 || idx >= len(files) {
		http.Error(w, "File not found or expired", http.StatusNotFound)
		return
	}

	fileInfo := files[idx]

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileInfo.Name))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(fileInfo.Size, 10))

	http.ServeFile(w, r, fileInfo.Path)

	// Mark completed
	h.transfers.MarkFileCompleted(transferID, idx)
}

// POST /api/peers/refresh
func (h *ServerHandlers) HandleRefreshPeers(w http.ResponseWriter, r *http.Request) {
	h.cfg.RefreshIPs()
	h.discovery.BroadcastBeacon("ping")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ping_sent"})
}

// GET /api/transfers
func (h *ServerHandlers) HandleGetTransfers(w http.ResponseWriter, r *http.Request) {
	sessions := h.transfers.ListSessions()
	writeJSON(w, http.StatusOK, sessions)
}

// POST /api/transfer/request
func (h *ServerHandlers) HandleTransferRequest(w http.ResponseWriter, r *http.Request) {
	var req transfer.TransferRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}

	accepted := h.transfers.HandleIncomingRequest(req, remoteIP)
	resp := transfer.TransferDecisionPayload{
		TransferID: req.TransferID,
		Accepted:   accepted,
	}

	if !accepted {
		resp.Reason = "Declined by recipient"
	}

	writeJSON(w, http.StatusOK, resp)
}

// POST /api/transfer/decision
func (h *ServerHandlers) HandleTransferDecision(w http.ResponseWriter, r *http.Request) {
	var dec transfer.TransferDecisionPayload
	if err := json.NewDecoder(r.Body).Decode(&dec); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ok := h.transfers.SetDecision(dec.TransferID, dec.Accepted)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "Session not found or already decided")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// POST /api/transfer/upload?transfer_id=...
func (h *ServerHandlers) HandleTransferUpload(w http.ResponseWriter, r *http.Request) {
	transferID := r.URL.Query().Get("transfer_id")
	if transferID == "" {
		writeJSONError(w, http.StatusBadRequest, "Missing transfer_id query parameter")
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Expected multipart/form-data: "+err.Error())
		return
	}

	fileIndex := 0

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "Failed to read part: "+err.Error())
			return
		}

		formName := part.FormName()
		if formName == "index" {
			data, _ := io.ReadAll(part)
			if idx, err := strconv.Atoi(string(data)); err == nil {
				fileIndex = idx
			}
			continue
		}

		if formName == "file" {
			fileName := part.FileName()
			savedPath, err := h.transfers.SaveUploadedFile(transferID, fileIndex, fileName, part)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "Failed to save file: "+err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{
				"status":     "uploaded",
				"saved_path": savedPath,
			})
			return
		}
	}

	writeJSONError(w, http.StatusBadRequest, "No file uploaded in part")
}

// POST /api/send-direct
// Used by web UI when sender selects files locally in browser and picks a target peer
func (h *ServerHandlers) HandleSendDirect(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form up to 2GB in temp files
	err := r.ParseMultipartForm(32 << 20) // 32MB in RAM, rest to temp files
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Failed to parse multipart form: "+err.Error())
		return
	}
	defer func() {
		_ = r.MultipartForm.RemoveAll()
	}()

	targetIP := r.FormValue("target_ip")
	targetPortStr := r.FormValue("target_port")
	if targetIP == "" || targetPortStr == "" {
		writeJSONError(w, http.StatusBadRequest, "target_ip and target_port required")
		return
	}

	targetPort, err := strconv.Atoi(targetPortStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid target_port")
		return
	}

	formFiles := r.MultipartForm.File["files"]
	if len(formFiles) == 0 {
		writeJSONError(w, http.StatusBadRequest, "No files provided")
		return
	}

	// Prepare local temp files for streaming
	tempDir, err := os.MkdirTemp("", "sendfile-tx-*")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to create temp staging dir")
		return
	}

	var localFiles []*transfer.LocalFileInfo
	for _, fh := range formFiles {
		src, err := fh.Open()
		if err != nil {
			_ = os.RemoveAll(tempDir)
			writeJSONError(w, http.StatusInternalServerError, "Failed to read file: "+err.Error())
			return
		}

		destPath := filepath.Join(tempDir, filepath.Base(fh.Filename))
		dst, err := os.Create(destPath)
		if err != nil {
			src.Close()
			_ = os.RemoveAll(tempDir)
			writeJSONError(w, http.StatusInternalServerError, "Failed to stage file: "+err.Error())
			return
		}

		_, err = io.Copy(dst, src)
		src.Close()
		dst.Close()
		if err != nil {
			_ = os.RemoveAll(tempDir)
			writeJSONError(w, http.StatusInternalServerError, "Failed to copy file: "+err.Error())
			return
		}

		localFiles = append(localFiles, &transfer.LocalFileInfo{
			Path:     destPath,
			Name:     fh.Filename,
			Size:     fh.Size,
			MimeType: fh.Header.Get("Content-Type"),
		})
	}

	targetPeerID := r.FormValue("target_peer_id")
	targetPeer, isKnown := h.peers.Get(targetPeerID)

	if isKnown && targetPeer.IsWeb {
		// Target is a phone / web browser client!
		transferID := fmt.Sprintf("web_tx_%d", time.Now().UnixNano())

		h.stagedMu.Lock()
		h.stagedWebDownloads[transferID] = localFiles
		h.stagedMu.Unlock()

		var items []*transfer.FileItem
		var totalBytes int64
		for idx, fi := range localFiles {
			items = append(items, &transfer.FileItem{
				Index: idx,
				Name:  fi.Name,
				Size:  fi.Size,
				Type:  fi.MimeType,
			})
			totalBytes += fi.Size
		}

		session := &transfer.TransferSession{
			ID:               transferID,
			Direction:        "send",
			PeerID:           targetPeer.ID,
			PeerName:         targetPeer.Name,
			PeerIP:           targetPeer.IP,
			Files:            items,
			TotalBytes:       totalBytes,
			TransferredBytes: 0,
			Status:           transfer.StatusTransferring,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}
		h.transfers.RegisterCustomSession(session)

		// Broadcast offer to the phone over SSE
		h.sse.Broadcast("web_download_offer", map[string]interface{}{
			"transfer_id":      transferID,
			"target_client_id": targetPeer.ID,
			"sender_name":      h.cfg.GetDeviceName(),
			"files":            items,
			"total_bytes":      totalBytes,
		})

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "transfer_initiated",
			"files":  len(localFiles),
			"target": targetPeer.Name,
			"is_web": true,
		})
		return
	}

	// Send to target native app peer asynchronously
	go func() {
		defer os.RemoveAll(tempDir)
		_, _ = h.transfers.SendFiles(targetIP, targetPort, localFiles)
	}()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "transfer_initiated",
		"files":  len(localFiles),
		"target": fmt.Sprintf("%s:%d", targetIP, targetPort),
	})
}

// POST /api/direct-upload
// Used when a phone or guest browser directly uploads files to this node
func (h *ServerHandlers) HandleDirectGuestUpload(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(32 << 20) // 32MB in memory, rest to temp disk
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Failed to parse form: "+err.Error())
		return
	}
	defer func() {
		_ = r.MultipartForm.RemoveAll()
	}()

	remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}

	formFiles := r.MultipartForm.File["files"]
	if len(formFiles) == 0 {
		// Fallback to "file" field name
		formFiles = r.MultipartForm.File["file"]
	}

	if len(formFiles) == 0 {
		writeJSONError(w, http.StatusBadRequest, "No files uploaded")
		return
	}

	clientName := r.UserAgent()
	if strings.Contains(clientName, "iPhone") {
		clientName = "Apple iPhone"
	} else if strings.Contains(clientName, "Android") {
		clientName = "Android Device"
	} else if strings.Contains(clientName, "iPad") {
		clientName = "Apple iPad"
	} else if strings.Contains(clientName, "Mac") {
		clientName = "Mac Device"
	} else {
		clientName = "Guest Device (" + remoteIP + ")"
	}

	clientID := r.FormValue("client_id")
	if clientID == "" {
		clientID = "guest_" + remoteIP
	}

	transferID := r.FormValue("transfer_id")
	if transferID == "" {
		transferID = r.URL.Query().Get("transfer_id")
	}

	session, hasSession := h.transfers.GetSession(transferID)
	if !hasSession || (session.Status != transfer.StatusAccepted && session.Status != transfer.StatusTransferring) {
		// Session was not pre-accepted via lightweight handshake; prompt destination now
		if transferID == "" {
			transferID = fmt.Sprintf("guest_%d", time.Now().UnixNano())
		}

		var items []*transfer.FileItem
		var totalBytes int64

		for idx, fh := range formFiles {
			items = append(items, &transfer.FileItem{
				Index: idx,
				Name:  fh.Filename,
				Size:  fh.Size,
				Type:  fh.Header.Get("Content-Type"),
			})
			totalBytes += fh.Size
		}

		// Request confirmation from host
		accepted := h.transfers.HandleIncomingRequest(transfer.TransferRequestPayload{
			TransferID: transferID,
			SenderID:   clientID,
			SenderName: clientName,
			SenderOS:   "mobile",
			Files:      items,
			TotalBytes: totalBytes,
		}, remoteIP)

		if !accepted {
			writeJSONError(w, http.StatusForbidden, "Transfer declined by recipient")
			return
		}
	}

	// 2. Save each file
	var savedFiles []string
	for idx, fh := range formFiles {
		src, err := fh.Open()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "Failed to read file: "+err.Error())
			return
		}

		savedPath, err := h.transfers.SaveUploadedFile(transferID, idx, fh.Filename, src)
		src.Close()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "Failed to save file: "+err.Error())
			return
		}
		savedFiles = append(savedFiles, savedPath)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "success",
		"saved_files": savedFiles,
		"count":       len(savedFiles),
	})
}

// POST /api/open-folder
func (h *ServerHandlers) HandleOpenFolder(w http.ResponseWriter, r *http.Request) {
	saveDir := h.cfg.GetSaveDir()
	_ = os.MkdirAll(saveDir, 0755)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", saveDir)
	case "darwin":
		cmd = exec.Command("open", saveDir)
	default:
		cmd = exec.Command("xdg-open", saveDir)
	}

	_ = cmd.Start()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"path":    saveDir,
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
