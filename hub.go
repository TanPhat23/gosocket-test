package main

import (
	"context"
	"log"
	"sync"
)

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	done       chan struct{}
	stopOnce   sync.Once
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

// Stop stops the Hub and initiates client cleanup.
func (h *Hub) Stop() {
	h.stopOnce.Do(func() {
		close(h.done)
	})
}

// Run starts the hub's main loop with support for optional context cancellation.
func (h *Hub) Run(ctx ...context.Context) {
	var ctxDone <-chan struct{}
	if len(ctx) > 0 && ctx[0] != nil {
		ctxDone = ctx[0].Done()
	}

	for {
		select {
		case <-ctxDone:
			h.cleanup()
			return
		case <-h.done:
			h.cleanup()
			return
		case client := <-h.register:
			h.clients[client] = true
			log.Printf("Client registered: %p", client)
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.msg)
				client.Close()
				log.Printf("Client unregistered: %p", client)
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.msg <- message:
				default:
					delete(h.clients, client)
					close(client.msg)
					client.Close()
					log.Printf("Client removed (buffer full): %p", client)
				}
			}
		}
	}
}

func (h *Hub) cleanup() {
	for client := range h.clients {
		delete(h.clients, client)
		close(client.msg)
		client.Close()
	}
	log.Println("Hub stopped and all clients cleaned up")
}
