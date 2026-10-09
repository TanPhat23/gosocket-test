package ws

import (
	"errors"
	"sync"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"github.com/fasthttp/websocket"
)

// Client represents a connected WebSocket subscriber.
type Client interface {
	ID() string
	TaskID() string
	Send(msg []byte) error
	Close() error
}

// Hub manages client connections and task room subscriptions.
type Hub struct {
	// rooms stores taskID -> map[string]Client (or SafeMap of clients)
	rooms *common.SafeMap[string, *common.SafeMap[string, Client]]

	register   chan Client
	unregister chan Client
	stop       chan struct{}
	once       sync.Once
}

// NewHub constructs a new Hub instance.
func NewHub() *Hub {
	return &Hub{
		rooms:      common.NewSafeMap[string, *common.SafeMap[string, Client]](),
		register:   make(chan Client, 256),
		unregister: make(chan Client, 256),
		stop:       make(chan struct{}),
	}
}

// Run starts the hub event loop.
func (h *Hub) Run() {
	for {
		select {
		case <-h.stop:
			return
		case client := <-h.register:
			h.addClient(client)
		case client := <-h.unregister:
			h.removeClient(client)
		}
	}
}

// Stop terminates the hub event loop.
func (h *Hub) Stop() {
	h.once.Do(func() {
		close(h.stop)
	})
}

// Register enqueues a client registration.
func (h *Hub) Register(client Client) {
	if client == nil {
		return
	}
	h.register <- client
}

// Unregister enqueues a client unregistration.
func (h *Hub) Unregister(client Client) {
	if client == nil {
		return
	}
	h.unregister <- client
}

func (h *Hub) addClient(client Client) {
	taskID := client.TaskID()
	room, _ := h.rooms.LoadOrStore(taskID, common.NewSafeMap[string, Client]())
	room.Store(client.ID(), client)
}

func (h *Hub) removeClient(client Client) {
	taskID := client.TaskID()
	if room, ok := h.rooms.Load(taskID); ok {
		room.Delete(client.ID())
	}
}

// BroadcastToTask sends raw byte payload to all clients subscribed to taskID.
func (h *Hub) BroadcastToTask(taskID string, data []byte) {
	room, ok := h.rooms.Load(taskID)
	if !ok {
		return
	}

	room.Range(func(clientID string, client Client) bool {
		// Send non-blocking or handle disconnects
		go func(c Client) {
			_ = c.Send(data)
		}(client)
		return true
	})
}

// BroadcastTaskLog marshals and broadcasts a log chunk message to subscribers of taskID.
func (h *Hub) BroadcastTaskLog(taskID string, chunk string) error {
	msg := NewLogMessage(taskID, chunk)
	data, err := common.Marshal(msg)
	if err != nil {
		return err
	}
	h.BroadcastToTask(taskID, data)
	return nil
}

// BroadcastGlobal broadcasts payload to all clients subscribed to the "global" events room.
func (h *Hub) BroadcastGlobal(data []byte) {
	h.BroadcastToTask("global", data)
}

// BroadcastTaskStatus marshals and broadcasts a status update message to subscribers of taskID and global event listeners.
func (h *Hub) BroadcastTaskStatus(taskID string, status models.TaskStatus) error {
	msg := NewStatusMessage(taskID, status)
	data, err := common.Marshal(msg)
	if err != nil {
		return err
	}
	h.BroadcastToTask(taskID, data)
	h.BroadcastToTask("global", data)
	return nil
}

// BroadcastTaskDiff marshals and broadcasts a diff message to subscribers of taskID.
func (h *Hub) BroadcastTaskDiff(taskID string, diff string) error {
	msg := NewDiffMessage(taskID, diff)
	data, err := common.Marshal(msg)
	if err != nil {
		return err
	}
	h.BroadcastToTask(taskID, data)
	return nil
}

// BroadcastTaskEvent marshals and broadcasts a structured agent event to subscribers of taskID.
func (h *Hub) BroadcastTaskEvent(taskID string, event any) error {
	msg := NewAgentEventMessage(taskID, event)
	data, err := common.Marshal(msg)
	if err != nil {
		return err
	}
	h.BroadcastToTask(taskID, data)
	return nil
}

// MockClient is a helper implementation of Client for testing and decoupling.
type MockClient struct {
	id       string
	taskID   string
	messages chan []byte
	closed   bool
	mu       sync.Mutex
}

// NewMockClient creates a new MockClient.
func NewMockClient(id string, taskID string) *MockClient {
	return &MockClient{
		id:       id,
		taskID:   taskID,
		messages: make(chan []byte, 100),
	}
}

func (m *MockClient) ID() string {
	return m.id
}

func (m *MockClient) TaskID() string {
	return m.taskID
}

func (m *MockClient) Send(msg []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("client closed")
	}
	m.messages <- msg
	return nil
}

func (m *MockClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.messages)
	}
	return nil
}

// Messages exposes the mock client's received message channel.
func (m *MockClient) Messages() <-chan []byte {
	return m.messages
}

// ConnClient wraps a live fasthttp WebSocket connection as a Client.
type ConnClient struct {
	id     string
	taskID string
	conn   *websocket.Conn
	mu     sync.Mutex
}

// NewConnClient creates a new WebSocket ConnClient.
func NewConnClient(id, taskID string, conn *websocket.Conn) *ConnClient {
	return &ConnClient{id: id, taskID: taskID, conn: conn}
}

func (c *ConnClient) ID() string     { return c.id }
func (c *ConnClient) TaskID() string { return c.taskID }

func (c *ConnClient) Send(msg []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteMessage(websocket.TextMessage, msg)
}

func (c *ConnClient) Close() error {
	return c.conn.Close()
}
