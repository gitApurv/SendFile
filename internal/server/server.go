package server

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sendfile/internal/config"
	"sendfile/internal/discovery"
	"sendfile/internal/transfer"
	"strings"
	"time"
)

type Server struct {
	cfg       *config.Config
	peers     *discovery.PeerTable
	disc      *discovery.DiscoveryService
	transfers *transfer.TransferManager
	sse       *SSEBroker
	handlers  *ServerHandlers
	httpSrv   *http.Server
	listener  net.Listener
	staticFS  fs.FS
}

func NewServer(
	cfg *config.Config,
	peers *discovery.PeerTable,
	disc *discovery.DiscoveryService,
	transfers *transfer.TransferManager,
	staticFS fs.FS,
) *Server {
	sse := NewSSEBroker()
	handlers := NewServerHandlers(cfg, peers, disc, transfers, sse)

	return &Server{
		cfg:       cfg,
		peers:     peers,
		disc:      disc,
		transfers: transfers,
		sse:       sse,
		handlers:  handlers,
		staticFS:  staticFS,
	}
}

func (s *Server) GetSSEBroker() *SSEBroker {
	return s.sse
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/info", s.handlers.HandleGetInfo)
	mux.HandleFunc("/api/settings", s.handlers.HandleUpdateSettings)
	mux.HandleFunc("/api/peers", s.handlers.HandleGetPeers)
	mux.HandleFunc("/api/peers/refresh", s.handlers.HandleRefreshPeers)
	mux.HandleFunc("/api/peers/register-web", s.handlers.HandleRegisterWebPeer)
	mux.HandleFunc("/api/peers/unregister-web", s.handlers.HandleUnregisterWebPeer)
	mux.HandleFunc("/api/download", s.handlers.HandleDownloadFile)
	mux.HandleFunc("/api/transfers", s.handlers.HandleGetTransfers)
	mux.HandleFunc("/api/transfer/request", s.handlers.HandleTransferRequest)
	mux.HandleFunc("/api/transfer/decision", s.handlers.HandleTransferDecision)
	mux.HandleFunc("/api/transfer/upload", s.handlers.HandleTransferUpload)
	mux.HandleFunc("/api/send-direct", s.handlers.HandleSendDirect)
	mux.HandleFunc("/api/direct-upload", s.handlers.HandleDirectGuestUpload)
	mux.HandleFunc("/api/open-folder", s.handlers.HandleOpenFolder)
	mux.HandleFunc("/api/events", s.sse.ServeHTTP)

	// Embedded Static UI files
	if s.staticFS != nil {
		fileServer := http.FileServer(http.FS(s.staticFS))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// Don't intercept API routes
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			fileServer.ServeHTTP(w, r)
		})
	}

	handler := corsMiddleware(mux)

	// Attempt binding to configured port or find an open port nearby
	var ln net.Listener
	var err error
	port := s.cfg.HTTPPort
	maxAttempts := 10

	for i := 0; i < maxAttempts; i++ {
		addr := fmt.Sprintf("0.0.0.0:%d", port+i)
		ln, err = net.Listen("tcp", addr)
		if err == nil {
			s.cfg.HTTPPort = port + i
			break
		}
	}

	if err != nil {
		return fmt.Errorf("failed to bind HTTP server to ports %d-%d: %w", port, port+maxAttempts, err)
	}

	s.listener = ln
	s.httpSrv = &http.Server{
		Handler:      handler,
		ReadTimeout:  0, // Allow indefinitely streaming large uploads
		WriteTimeout: 0,
	}

	log.Printf("[Server] HTTP File Transfer Server listening on http://localhost:%d (and http://%s:%d)",
		s.cfg.HTTPPort, s.cfg.PrimaryIP, s.cfg.HTTPPort)

	go func() {
		if err := s.httpSrv.Serve(s.listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[Server] Error running HTTP server: %v", err)
		}
	}()

	return nil
}

func (s *Server) Stop() {
	if s.httpSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(ctx)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Range, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
