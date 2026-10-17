package main

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512 * 1024 // 512KB
)

type Client struct {
	hub       *Hub
	conn      *websocket.Conn
	send      chan []byte
	closeOnce sync.Once

	// msg and done kept temporarily for compatibility with existing Hub before Task 2 refactors it
	msg  chan []byte
	done chan struct{}
}

// NewClient creates a new Client instance.
// Accepts (hub, conn) as canonical constructor, or (conn) for backward compatibility with main.go before Task 3.
func NewClient(args ...interface{}) *Client {
	var h *Hub
	var c *websocket.Conn
	if len(args) == 1 {
		if conn, ok := args[0].(*websocket.Conn); ok {
			c = conn
		}
	} else if len(args) >= 2 {
		if hub, ok := args[0].(*Hub); ok {
			h = hub
		}
		if conn, ok := args[1].(*websocket.Conn); ok {
			c = conn
		}
	}
	send := make(chan []byte, 256)
	return &Client{
		hub:       h,
		conn:      c,
		send:      send,
		msg:       send,
		done:      make(chan struct{}),
		closeOnce: sync.Once{},
	}
}

// Read is kept for backward compatibility with main.go until Task 3 refactors it.
func (c *Client) Read(hub *Hub) {
	c.ReadPump()
}

// Write is kept for backward compatibility with main.go until Task 3 refactors it.
func (c *Client) Write() {
	c.WritePump()
}

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.send)
		c.conn.Close()
	})
}

func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		c.hub.broadcast <- message
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message.
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
