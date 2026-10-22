package transfer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sendfile/internal/config"
	"strconv"
	"strings"
	"sync"
	"time"
)

type TransferManager struct {
	mu               sync.RWMutex
	cfg              *config.Config
	sessions         map[string]*TransferSession
	pendingDecisions map[string]chan bool
	onUpdate         func(*TransferSession)
	httpClient       *http.Client
}

func NewTransferManager(cfg *config.Config, onUpdate func(*TransferSession)) *TransferManager {
	return &TransferManager{
		cfg:              cfg,
		sessions:         make(map[string]*TransferSession),
		pendingDecisions: make(map[string]chan bool),
		onUpdate:         onUpdate,
		httpClient: &http.Client{
			Timeout: 0, // No timeout for large transfers
		},
	}
}

func (tm *TransferManager) GetSession(id string) (*TransferSession, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	s, exists := tm.sessions[id]
	if !exists {
		return nil, false
	}
	cp := *s
	return &cp, true
}

func (tm *TransferManager) ListSessions() []*TransferSession {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	list := make([]*TransferSession, 0, len(tm.sessions))
	for _, s := range tm.sessions {
		cp := *s
		list = append(list, &cp)
	}
	return list
}

func (tm *TransferManager) RegisterCustomSession(s *TransferSession) {
	tm.mu.Lock()
	tm.sessions[s.ID] = s
	tm.mu.Unlock()
	tm.notify(s)
}

func (tm *TransferManager) MarkFileCompleted(sessionID string, fileIndex int) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	s, ok := tm.sessions[sessionID]
	if !ok {
		return
	}

	if fileIndex >= 0 && fileIndex < len(s.Files) {
		s.Files[fileIndex].Completed = true
		s.Files[fileIndex].Transferred = s.Files[fileIndex].Size
	}

	var transferred int64
	allComplete := true
	for _, f := range s.Files {
		transferred += f.Transferred
		if !f.Completed {
			allComplete = false
		}
	}
	s.TransferredBytes = transferred
	if allComplete {
		s.Status = StatusCompleted
		s.ETASeconds = 0
	}
	s.UpdatedAt = time.Now()
	tm.notify(s)
}

func (tm *TransferManager) HandleIncomingRequest(req TransferRequestPayload, remoteIP string) bool {
	tm.mu.Lock()
	session := &TransferSession{
		ID:               req.TransferID,
		Direction:        "receive",
		PeerID:           req.SenderID,
		PeerName:         req.SenderName,
		PeerIP:           remoteIP,
		Files:            req.Files,
		TotalBytes:       req.TotalBytes,
		TransferredBytes: 0,
		Status:           StatusPending,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	tm.sessions[session.ID] = session

	decisionCh := make(chan bool, 1)
	tm.pendingDecisions[session.ID] = decisionCh
	tm.mu.Unlock()

	tm.notify(session)

	if tm.cfg.IsAutoAccept() {
		tm.SetDecision(session.ID, true)
		return true
	}

	// Wait up to 60 seconds for user action from UI
	select {
	case accepted := <-decisionCh:
		return accepted
	case <-time.After(60 * time.Second):
		tm.SetDecision(session.ID, false)
		return false
	}
}

func (tm *TransferManager) SetDecision(sessionID string, accepted bool) bool {
	tm.mu.Lock()
	ch, exists := tm.pendingDecisions[sessionID]
	session, hasSession := tm.sessions[sessionID]
	if hasSession {
		if accepted {
			session.Status = StatusAccepted
		} else {
			session.Status = StatusRejected
		}
		session.UpdatedAt = time.Now()
	}
	tm.mu.Unlock()

	if hasSession {
		tm.notify(session)
	}

	if exists {
		select {
		case ch <- accepted:
		default:
		}
		tm.mu.Lock()
		delete(tm.pendingDecisions, sessionID)
		tm.mu.Unlock()
		return true
	}
	return false
}

func (tm *TransferManager) SaveUploadedFile(sessionID string, fileIndex int, filename string, r io.Reader) (string, error) {
	tm.mu.RLock()
	session, exists := tm.sessions[sessionID]
	tm.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("session %s not found", sessionID)
	}

	cleanName := filepath.Base(filename)
	if cleanName == "" || cleanName == "." || cleanName == "/" {
		cleanName = "file_" + strconv.Itoa(fileIndex)
	}

	saveDir := tm.cfg.GetSaveDir()
	targetPath := getUniqueFilePath(saveDir, cleanName)
	partPath := targetPath + ".part"

	out, err := os.Create(partPath)
	if err != nil {
		return "", fmt.Errorf("failed to create destination file: %w", err)
	}

	tm.updateSessionStatus(sessionID, StatusTransferring)

	// Stream with progress tracking
	startTime := time.Now()
	lastReport := time.Now()
	var lastBytes int64

	buf := make([]byte, 64*1024) // 64KB buffer
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				_ = out.Close()
				_ = os.Remove(partPath)
				return "", writeErr
			}

			tm.mu.Lock()
			session.TransferredBytes += int64(n)
			if fileIndex < len(session.Files) {
				session.Files[fileIndex].Transferred += int64(n)
			}

			now := time.Now()
			elapsedSinceLast := now.Sub(lastReport).Seconds()
			if elapsedSinceLast >= 0.5 {
				bytesSinceLast := session.TransferredBytes - lastBytes
				session.SpeedBytesPerSec = float64(bytesSinceLast) / elapsedSinceLast
				remainingBytes := session.TotalBytes - session.TransferredBytes
				if session.SpeedBytesPerSec > 0 {
					session.ETASeconds = int(float64(remainingBytes) / session.SpeedBytesPerSec)
				}
				lastReport = now
				lastBytes = session.TransferredBytes
			}
			session.UpdatedAt = now
			tm.mu.Unlock()

			tm.notify(session)
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			_ = out.Close()
			_ = os.Remove(partPath)
			tm.updateSessionStatus(sessionID, StatusFailed)
			return "", readErr
		}
	}

	_ = out.Close()

	// Rename .part to final target file
	if err := os.Rename(partPath, targetPath); err != nil {
		return "", fmt.Errorf("failed to finalize file: %w", err)
	}

	tm.mu.Lock()
	if fileIndex < len(session.Files) {
		session.Files[fileIndex].Completed = true
		session.Files[fileIndex].SavedPath = targetPath
	}

	allComplete := true
	for _, f := range session.Files {
		if !f.Completed {
			allComplete = false
			break
		}
	}

	totalDuration := time.Since(startTime).Seconds()
	if totalDuration > 0 {
		session.SpeedBytesPerSec = float64(session.TransferredBytes) / totalDuration
	}
	session.ETASeconds = 0

	if allComplete {
		session.Status = StatusCompleted
	}
	session.UpdatedAt = time.Now()
	tm.mu.Unlock()

	tm.notify(session)
	return targetPath, nil
}

func (tm *TransferManager) SendFiles(targetIP string, targetPort int, files []*LocalFileInfo) (*TransferSession, error) {
	transferID := fmt.Sprintf("tx_%d", time.Now().UnixNano())

	var items []*FileItem
	var totalBytes int64

	for i, f := range files {
		item := &FileItem{
			Index: i,
			Name:  f.Name,
			Size:  f.Size,
			Type:  f.MimeType,
		}
		items = append(items, item)
		totalBytes += f.Size
	}

	session := &TransferSession{
		ID:               transferID,
		Direction:        "send",
		PeerIP:           targetIP,
		Files:            items,
		TotalBytes:       totalBytes,
		TransferredBytes: 0,
		Status:           StatusPending,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	tm.mu.Lock()
	tm.sessions[transferID] = session
	tm.mu.Unlock()
	tm.notify(session)

	// Step 1: Request permission
	reqPayload := TransferRequestPayload{
		TransferID: transferID,
		SenderID:   tm.cfg.NodeID,
		SenderName: tm.cfg.GetDeviceName(),
		SenderOS:   tm.cfg.OS,
		Files:      items,
		TotalBytes: totalBytes,
	}

	bodyBytes, _ := json.Marshal(reqPayload)
	reqURL := fmt.Sprintf("http://%s:%d/api/transfer/request", targetIP, targetPort)

	httpReq, err := http.NewRequestWithContext(context.Background(), "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		tm.markSessionError(transferID, err.Error())
		return session, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := tm.httpClient.Do(httpReq)
	if err != nil {
		tm.markSessionError(transferID, "Failed to connect to recipient: "+err.Error())
		return session, err
	}
	defer resp.Body.Close()

	var decision TransferDecisionPayload
	if err := json.NewDecoder(resp.Body).Decode(&decision); err != nil {
		tm.markSessionError(transferID, "Invalid response from recipient")
		return session, err
	}

	if !decision.Accepted {
		tm.updateSessionStatus(transferID, StatusRejected)
		return session, fmt.Errorf("transfer declined by recipient")
	}

	tm.updateSessionStatus(transferID, StatusTransferring)

	// Step 2: Upload files
	tm.executeUploads(targetIP, targetPort, transferID, files)

	return session, nil
}

func (tm *TransferManager) executeUploads(targetIP string, targetPort int, transferID string, files []*LocalFileInfo) {
	uploadURL := fmt.Sprintf("http://%s:%d/api/transfer/upload?transfer_id=%s", targetIP, targetPort, transferID)

	for i, f := range files {
		fileReader, err := os.Open(f.Path)
		if err != nil {
			tm.markSessionError(transferID, "Failed to open local file: "+err.Error())
			return
		}

		bodyReader, bodyWriter := io.Pipe()
		mpWriter := multipart.NewWriter(bodyWriter)
		contentType := mpWriter.FormDataContentType()

		go func(idx int, fi *LocalFileInfo, r *os.File) {
			defer bodyWriter.Close()
			defer mpWriter.Close()
			defer r.Close()

			// Write index first
			_ = mpWriter.WriteField("index", strconv.Itoa(idx))

			part, err := mpWriter.CreateFormFile("file", fi.Name)
			if err != nil {
				_ = bodyWriter.CloseWithError(err)
				return
			}

			buf := make([]byte, 64*1024)
			lastReport := time.Now()
			var lastBytes int64

			for {
				n, err := r.Read(buf)
				if n > 0 {
					if _, wErr := part.Write(buf[:n]); wErr != nil {
						_ = bodyWriter.CloseWithError(wErr)
						return
					}

					tm.mu.Lock()
					if s, ok := tm.sessions[transferID]; ok {
						s.TransferredBytes += int64(n)
						if idx < len(s.Files) {
							s.Files[idx].Transferred += int64(n)
						}
						now := time.Now()
						elapsed := now.Sub(lastReport).Seconds()
						if elapsed >= 0.5 {
							delta := s.TransferredBytes - lastBytes
							s.SpeedBytesPerSec = float64(delta) / elapsed
							rem := s.TotalBytes - s.TransferredBytes
							if s.SpeedBytesPerSec > 0 {
								s.ETASeconds = int(float64(rem) / s.SpeedBytesPerSec)
							}
							lastReport = now
							lastBytes = s.TransferredBytes
						}
						s.UpdatedAt = now
						tm.notify(s)
					}
					tm.mu.Unlock()
				}
				if err != nil {
					if err == io.EOF {
						break
					}
					_ = bodyWriter.CloseWithError(err)
					return
				}
			}
		}(i, f, fileReader)

		req, err := http.NewRequest("POST", uploadURL, bodyReader)
		if err != nil {
			tm.markSessionError(transferID, err.Error())
			return
		}
		req.Header.Set("Content-Type", contentType)

		resp, err := tm.httpClient.Do(req)
		if err != nil {
			tm.markSessionError(transferID, "Upload failed: "+err.Error())
			return
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			tm.markSessionError(transferID, fmt.Sprintf("Recipient returned status %d", resp.StatusCode))
			return
		}

		tm.mu.Lock()
		if s, ok := tm.sessions[transferID]; ok {
			if i < len(s.Files) {
				s.Files[i].Completed = true
			}
			tm.notify(s)
		}
		tm.mu.Unlock()
	}

	tm.mu.Lock()
	if s, ok := tm.sessions[transferID]; ok {
		s.Status = StatusCompleted
		s.ETASeconds = 0
		s.UpdatedAt = time.Now()
		tm.notify(s)
	}
	tm.mu.Unlock()
}

func (tm *TransferManager) updateSessionStatus(id string, status SessionStatus) {
	tm.mu.Lock()
	if s, ok := tm.sessions[id]; ok {
		s.Status = status
		s.UpdatedAt = time.Now()
		tm.notify(s)
	}
	tm.mu.Unlock()
}

func (tm *TransferManager) markSessionError(id string, errMsg string) {
	tm.mu.Lock()
	if s, ok := tm.sessions[id]; ok {
		s.Status = StatusFailed
		s.Error = errMsg
		s.UpdatedAt = time.Now()
		tm.notify(s)
	}
	tm.mu.Unlock()
}

func (tm *TransferManager) notify(s *TransferSession) {
	if tm.onUpdate != nil {
		cp := *s
		go tm.onUpdate(&cp)
	}
}

type LocalFileInfo struct {
	Path     string
	Name     string
	Size     int64
	MimeType string
}

func getUniqueFilePath(dir, filename string) string {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	target := filepath.Join(dir, filename)

	counter := 1
	for {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			return target
		}
		target = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, counter, ext))
		counter++
	}
}
