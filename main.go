package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type Config struct {
	Addr           string
	AllowedOrigins []string
}

type Server struct {
	hub        *Hub
	httpServer *http.Server
	upgrader   websocket.Upgrader
}

func checkOrigin(allowedOrigins []string) func(r *http.Request) bool {
	// Normalize configured origins
	allowedMap := make(map[string]struct{})
	for _, origin := range allowedOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed != "" {
			allowedMap[strings.ToLower(trimmed)] = struct{}{}
		}
	}

	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// Non-browser or same-origin requests without Origin header
			return true
		}

		u, err := url.Parse(origin)
		if err != nil {
			return false
		}

		// Same-origin check: compare host with r.Host
		if strings.EqualFold(u.Host, r.Host) {
			return true
		}

		// Configured allowed origins check (exact origin match e.g. "https://example.com" or host match)
		originLower := strings.ToLower(origin)
		if _, ok := allowedMap[originLower]; ok {
			return true
		}
		if _, ok := allowedMap[strings.ToLower(u.Host)]; ok {
			return true
		}

		return false
	}
}

func NewServer(cfg Config) *Server {
	var origins []string
	if len(cfg.AllowedOrigins) > 0 {
		origins = append(origins, cfg.AllowedOrigins...)
	}

	if envOrigins := os.Getenv("ALLOWED_ORIGINS"); envOrigins != "" {
		for _, o := range strings.Split(envOrigins, ",") {
			trimmed := strings.TrimSpace(o)
			if trimmed != "" {
				origins = append(origins, trimmed)
			}
		}
	}

	h := NewHub()
	go h.Run()

	s := &Server{
		hub: h,
	}

	s.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     checkOrigin(origins),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.HandleSocket)

	addr := cfg.Addr
	if addr == "" {
		addr = ":8080"
	}

	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}

	return s
}

func (s *Server) HandleSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}
	client := NewClient(s.hub, conn)
	s.hub.register <- client
	go client.ReadPump()
	go client.WritePump()
}

func (s *Server) Start() error {
	log.Printf("Server starting on %s", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	// Stop accepting new HTTP requests and connections first
	err := s.httpServer.Shutdown(ctx)

	// Close all hub connections and client resources
	s.hub.Stop()

	return err
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	cfg := Config{
		Addr: ":" + port,
	}
	server := NewServer(cfg)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := server.Start(); err != nil {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	fmt.Printf("Server started on %s\n", server.httpServer.Addr)
	<-stop
	fmt.Println("\nShutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	fmt.Println("Server exited cleanly")
}
