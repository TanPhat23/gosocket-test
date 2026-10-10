package main

import (
	"context"
	"log"
)

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	done       chan struct{}
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		done:       make(chan struct{}),
	}
}

func (h *Hub) Run() {
	h.RunWithContext(context.Background())
}

func (h *Hub) RunWithContext(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.cleanup()
			return
		case <-h.done:
			h.cleanup()
			return
		case client := <-h.register:
			h.clients[client] = true
			log.Printf("Client registered: %v", client)
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.msg)
				log.Printf("Client unregistered: %v", client)
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.msg <- message:
				default:
					delete(h.clients, client)
					close(client.msg)
					log.Printf("Client removed (buffer full): %v", client)
				}
			}
		}
	}
}

func (h *Hub) Stop() {
	select {
	case <-h.done:
	default:
		close(h.done)
	}
}

func (h *Hub) cleanup() {
	for client := range h.clients {
		delete(h.clients, client)
		close(client.msg)
	}
}
