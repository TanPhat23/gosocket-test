package main

import (
	"log"
	"sync"
)

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	quit       chan struct{}
	closeOnce  sync.Once
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		quit:       make(chan struct{}),
	}
}

// Stop stops the Hub and shuts down all connected clients.
func (h *Hub) Stop() {
	h.closeOnce.Do(func() {
		close(h.quit)
	})
}

func (h *Hub) removeClient(client *Client) {
	if _, ok := h.clients[client]; ok {
		delete(h.clients, client)
		close(client.msg)
		client.Close()
	}
}

func (h *Hub) Run() {
	for {
		select {
		case <-h.quit:
			for client := range h.clients {
				h.removeClient(client)
			}
			return

		case client := <-h.register:
			h.clients[client] = true
			log.Printf("Client registered: %p", client)

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				h.removeClient(client)
				log.Printf("Client unregistered: %p", client)
			}

		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.msg <- message:
				default:
					h.removeClient(client)
					log.Printf("Client removed (buffer full): %p", client)
				}
			}
		}
	}
}
