package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

type EventMessage struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

type SSEBroker struct {
	mu      sync.Mutex
	clients map[chan EventMessage]bool
}

func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients: make(map[chan EventMessage]bool),
	}
}

func (b *SSEBroker) AddClient(ch chan EventMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clients[ch] = true
}

func (b *SSEBroker) RemoveClient(ch chan EventMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.clients, ch)
	close(ch)
}

func (b *SSEBroker) Broadcast(event string, data interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()

	msg := EventMessage{
		Event: event,
		Data:  data,
	}

	for ch := range b.clients {
		select {
		case ch <- msg:
		default:
			// Non-blocking drop if client is too slow
		}
	}
}

func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	clientChan := make(chan EventMessage, 10)
	b.AddClient(clientChan)
	defer b.RemoveClient(clientChan)

	// Send initial ping
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-clientChan:
			if !ok {
				return
			}
			bytes, err := json.Marshal(msg.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", msg.Event, string(bytes))
			flusher.Flush()
		}
	}
}
