package main

import (
	"context"
	"errors"
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
		// Allow all connections by default or configure per requirements
		return true
	},
}

func handleSocket(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("websocket upgrade error: %v", err)
			return
		}
		client := NewClient(conn, hub)
		hub.register <- client
		go client.WritePump()
		go client.ReadPump()
	}
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// NewServer initializes an http.Server with configured endpoints and middleware.
func NewServer(addr string, hub *Hub) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", handleSocket(hub))
	mux.HandleFunc("/healthz", handleHealthz)

	return &http.Server{
		Addr:    addr,
		Handler: mux,
	}
}

func main() {
	hubCtx, cancelHub := context.WithCancel(context.Background())
	defer cancelHub()

	hub := NewHub()
	go hub.Run(hubCtx)

	server := NewServer(":8080", hub)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Server listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server ListenAndServe error: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down server...")

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	cancelHub()
	log.Println("Server exiting")
}
