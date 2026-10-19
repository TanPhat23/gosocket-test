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
		return true // Allow cross-origin connections for WebSocket client versatility
	},
}

type Server struct {
	hub        *Hub
	httpServer *http.Server
}

func NewServer(addr string, hub *Hub) *Server {
	mux := http.NewServeMux()
	s := &Server{
		hub: hub,
		httpServer: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}
	mux.HandleFunc("/ws", s.HandleSocket)
	return s
}

func (s *Server) HandleSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade websocket: %v", err)
		return
	}
	client := NewClient(conn)
	s.hub.register <- client
	go client.Read(s.hub)
	go client.Write()
}

func main() {
	hub := NewHub()
	go hub.Run()

	server := NewServer(":8080", hub)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Println("Server started on :8080")
		if err := server.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-stop
	fmt.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.httpServer.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
	hub.Stop()
	fmt.Println("Server gracefully stopped")
}
