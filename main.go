package main

import (
	"context"
	"errors"
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
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		// Allow requests with origin matching Host header
		if origin == "http://"+r.Host || origin == "https://"+r.Host {
			return true
		}
		return false
	},
}

var hub *Hub

func HandleSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}
	client := NewClient(conn)
	hub.register <- client
	go client.Read(hub)
	go client.Write()
}

func main() {
	hub = NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.RunWithContext(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", HandleSocket)
	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Println("Server started on :8080")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server listen error: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	cancel()
	log.Println("Server gracefully stopped")
}
