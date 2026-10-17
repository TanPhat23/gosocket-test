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
	shutdown   chan struct{}
	done       chan struct{}
	stopOnce   sync.Once
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *Client, 64),
		unregister: make(chan *Client, 64),
		shutdown:   make(chan struct{}),
		done:       make(chan struct{}),
	}
}

func (h *Hub) Run() {
	defer close(h.done)

	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			log.Printf("Client registered: %p", client)
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.Close()
				log.Printf("Client unregistered: %p", client)
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					delete(h.clients, client)
					client.Close()
					log.Printf("Client removed (buffer full): %p", client)
				}
			}
		case <-h.shutdown:
			for client := range h.clients {
				client.Close()
			}
			h.clients = make(map[*Client]bool)
			return
		}
	}
}

func (h *Hub) Stop() {
	h.stopOnce.Do(func() {
		close(h.shutdown)
	})
	<-h.done
}
