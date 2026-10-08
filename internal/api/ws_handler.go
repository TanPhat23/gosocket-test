package api

import (
	"context"
	"strings"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/service"
	"agent-task-manager/internal/ws"
	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

var wsUpgrader = websocket.FastHTTPUpgrader{
	CheckOrigin: func(ctx *fasthttp.RequestCtx) bool {
		return true
	},
}

// WSHandler handles WebSocket connections for terminal and log streaming.
type WSHandler struct {
	hub     *ws.Hub
	taskSvc service.TaskService
	logSvc  service.LogService
}

// NewWSHandler creates a new WSHandler.
func NewWSHandler(hub *ws.Hub, opts ...func(*WSHandler)) *WSHandler {
	h := &WSHandler{hub: hub}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// WithWSServices injects optional task and log services into WSHandler.
func WithWSServices(taskSvc service.TaskService, logSvc service.LogService) func(*WSHandler) {
	return func(h *WSHandler) {
		h.taskSvc = taskSvc
		h.logSvc = logSvc
	}
}

// HandleTerminalWS upgrades connection to WebSocket for terminal logs streaming.
func (h *WSHandler) HandleTerminalWS(c fiber.Ctx) error {
	return WebSocketLogsHandler(h.hub, h.taskSvc, h.logSvc)(c)
}

// HandleEventsWS upgrades connection to WebSocket for streaming all task status changes in real-time.
func (h *WSHandler) HandleEventsWS(c fiber.Ctx) error {
	err := wsUpgrader.Upgrade(c.RequestCtx(), func(conn *websocket.Conn) {
		clientID := common.NewID()
		client := ws.NewConnClient(clientID, "global", conn)
		h.hub.Register(client)
		defer func() {
			h.hub.Unregister(client)
			_ = client.Close()
		}()

		// Read loop to detect disconnects and handle pings
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	})
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "websocket upgrade failed"})
	}
	return nil
}

// WebSocketLogsHandler streams real-time terminal ANSI logs and task status events over WebSocket.
func WebSocketLogsHandler(hub *ws.Hub, taskSvc service.TaskService, logSvc service.LogService) fiber.Handler {
	return func(c fiber.Ctx) error {
		taskID := c.Params("task_id")
		if taskID == "" || strings.Contains(taskID, "/") {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "task_id is required"})
		}

		err := wsUpgrader.Upgrade(c.RequestCtx(), func(conn *websocket.Conn) {
			clientID := common.NewID()
			client := ws.NewConnClient(clientID, taskID, conn)
			hub.Register(client)
			defer func() {
				hub.Unregister(client)
				_ = client.Close()
			}()

			// Replay existing logs to newly connected client
			if logSvc != nil {
				logs, err := logSvc.GetLogs(context.Background(), taskID)
				if err == nil {
					for _, l := range logs {
						msg := ws.NewLogMessage(taskID, l.LogChunk)
						data, _ := common.Marshal(msg)
						if err := client.Send(data); err != nil {
							return
						}
					}
				}
			}

			// Read loop to detect disconnects and handle pings
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					break
				}
			}
		})
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "websocket upgrade failed"})
		}
		return nil
	}
}
