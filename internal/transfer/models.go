package transfer

import (
	"time"
)

type FileItem struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Type        string `json:"type"`
	Transferred int64  `json:"transferred"`
	Completed   bool   `json:"completed"`
	SavedPath   string `json:"saved_path,omitempty"`
}

type SessionStatus string

const (
	StatusPending      SessionStatus = "pending"
	StatusAccepted     SessionStatus = "accepted"
	StatusRejected     SessionStatus = "rejected"
	StatusTransferring SessionStatus = "transferring"
	StatusCompleted    SessionStatus = "completed"
	StatusFailed       SessionStatus = "failed"
	StatusCancelled    SessionStatus = "cancelled"
)

type TransferSession struct {
	ID               string        `json:"id"`
	Direction        string        `json:"direction"` // "send" or "receive"
	PeerID           string        `json:"peer_id"`
	PeerName         string        `json:"peer_name"`
	PeerIP           string        `json:"peer_ip"`
	Files            []*FileItem   `json:"files"`
	TotalBytes       int64         `json:"total_bytes"`
	TransferredBytes int64         `json:"transferred_bytes"`
	Status           SessionStatus `json:"status"`
	SpeedBytesPerSec float64       `json:"speed_bytes_per_sec"`
	ETASeconds       int           `json:"eta_seconds"`
	Error            string        `json:"error,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
}

type TransferRequestPayload struct {
	TransferID string      `json:"transfer_id"`
	SenderID   string      `json:"sender_id"`
	SenderName string      `json:"sender_name"`
	SenderOS   string      `json:"sender_os"`
	Files      []*FileItem `json:"files"`
	TotalBytes int64       `json:"total_bytes"`
}

type TransferDecisionPayload struct {
	TransferID string `json:"transfer_id"`
	Accepted   bool   `json:"accepted"`
	Reason     string `json:"reason,omitempty"`
}
