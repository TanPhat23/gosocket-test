package main

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Default time allowed to write a message to the peer.
	defaultWriteWait = 10 * time.Second

	// Default time allowed to read the next pong message from the peer.
	defaultPongWait = 60 * time.Second

	// Default send pings to peer with this period. Must be less than pongWait.
	defaultPingPeriod = (defaultPongWait * 9) / 10

	// Default maximum message size allowed from peer.
	defaultMaxMessageSize int64 = 512

	// Buffer size for client message channel.
	sendBufferSize = 256
)

type ClientConfig struct {
	WriteWait      time.Duration
	PongWait       time.Duration
	PingPeriod     time.Duration
	MaxMessageSize int64
}

func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		WriteWait:      defaultWriteWait,
		PongWait:       defaultPongWait,
		PingPeriod:     defaultPingPeriod,
		MaxMessageSize: defaultMaxMessageSize,
	}
}

type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	msg    chan []byte
	done   chan struct{}
	once   sync.Once
	config ClientConfig
}

func NewClient(hub *Hub, conn *websocket.Conn) *Client {
	return NewClientWithConfig(hub, conn, DefaultClientConfig())
}

func NewClientWithConfig(hub *Hub, conn *websocket.Conn, cfg ClientConfig) *Client {
	if cfg.WriteWait <= 0 {
		cfg.WriteWait = defaultWriteWait
	}
	if cfg.PongWait <= 0 {
		cfg.PongWait = defaultPongWait
	}
	if cfg.PingPeriod <= 0 {
		cfg.PingPeriod = (cfg.PongWait * 9) / 10
	}
	if cfg.MaxMessageSize <= 0 {
		cfg.MaxMessageSize = defaultMaxMessageSize
	}
	return &Client{
		hub:    hub,
		conn:   conn,
		msg:    make(chan []byte, sendBufferSize),
		done:   make(chan struct{}),
		config: cfg,
	}
}

// Close gracefully marks the client closed and cleans up channels and socket exactly once.
func (c *Client) Close() {
	c.once.Do(func() {
		close(c.done)
		if c.conn != nil {
			c.conn.Close()
		}
	})
}

// ReadPump pumps messages from the websocket connection to the hub.
func (c *Client) ReadPump() {
	defer func() {
		if c.hub != nil {
			c.hub.unregister <- c
		}
		c.Close()
	}()

	c.conn.SetReadLimit(c.config.MaxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(c.config.PongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.config.PongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		if c.hub != nil {
			c.hub.broadcast <- message
		}
	}
}

// WritePump pumps messages from the hub to the websocket connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(c.config.PingPeriod)
	defer func() {
		ticker.Stop()
		c.Close()
	}()

	for {
		select {
		case message, ok := <-c.msg:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.config.WriteWait))
			if !ok {
				// The hub closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if _, err := w.Write(message); err != nil {
				return
			}

			// Add queued chat messages to the current websocket message.
			n := len(c.msg)
			for i := 0; i < n; i++ {
				_, _ = w.Write([]byte{'\n'})
				_, _ = w.Write(<-c.msg)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.config.WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}

		case <-c.done:
			return
		}
	}
}

// Read is a backward-compatible wrapper for ReadPump.
func (c *Client) Read(hub *Hub) {
	c.hub = hub
	c.ReadPump()
}

// Write is a backward-compatible wrapper for WritePump.
func (c *Client) Write() {
	c.WritePump()
}
