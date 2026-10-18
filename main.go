package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var hub *Hub

// ServeWsWithConfig handles websocket requests with custom client configuration.
func ServeWsWithConfig(h *Hub, w http.ResponseWriter, r *http.Request, cfg ClientConfig) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade websocket: %v", err)
		return
	}
	client := NewClientWithConfig(h, conn, cfg)
	h.register <- client

	go client.WritePump()
	go client.ReadPump()
}

// ServeWs handles websocket requests with default configuration.
func ServeWs(h *Hub, w http.ResponseWriter, r *http.Request) {
	ServeWsWithConfig(h, w, r, DefaultClientConfig())
}

// HandleSocket provides backward compatibility with the default hub.
func HandleSocket(w http.ResponseWriter, r *http.Request) {
	ServeWs(hub, w, r)
}

// NewRouter creates an http.Handler with the given hub.
func NewRouter(h *Hub) http.Handler {
	return NewRouterWithConfig(h, DefaultClientConfig())
}

// NewRouterWithConfig creates an http.Handler with the given hub and client configuration.
func NewRouterWithConfig(h *Hub, cfg ClientConfig) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ServeWsWithConfig(h, w, r, cfg)
	})
	return mux
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hub = NewHub()
	go hub.Run(ctx)

	server := &http.Server{
		Addr:    ":8080",
		Handler: NewRouter(hub),
	}

	serverErr := make(chan error, 1)
	go func() {
		fmt.Println("Server started on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Println("Shutting down server gracefully...")
	case err := <-serverErr:
		log.Fatalf("Server error: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server Shutdown error: %v", err)
	}

	hub.Stop()
	log.Println("Server shutdown complete.")
}
