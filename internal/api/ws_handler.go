package api

import (
	"context"
	"net/url"
	"strings"
	"time"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/service"
	"agent-task-manager/internal/ws"
	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

// eventAuthzTimeout bounds the per-event visibility lookup for /ws/events subscribers.
const eventAuthzTimeout = 5 * time.Second

var wsUpgrader = websocket.FastHTTPUpgrader{
	CheckOrigin: wsOriginAllowed,
}

// wsOriginAllowed rejects cross-site WebSocket handshakes. The configuration has no dedicated
// frontend origin, so a browser Origin must name the same host[:port] the request was sent to
// (the frontend is served/proxied on the API host). Requests without an Origin header come from
// non-browser clients, which cannot be driven cross-site; they still need RequireAuth credentials.
func wsOriginAllowed(ctx *fasthttp.RequestCtx) bool {
	origin := ctx.Request.Header.Peek(fasthttp.HeaderOrigin)
	if len(origin) == 0 {
		return true
	}
	u, err := url.Parse(string(origin))
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, string(ctx.Host()))
}

// visibleTaskEventsClient wraps a global events subscriber and forwards only messages about tasks
// the subscribing user may view (RoleViewer+ on the task's team). Messages without a task ID or that
// cannot be authorized are dropped (fail closed). Send runs in its own goroutine per broadcast.
type visibleTaskEventsClient struct {
	ws.Client
	userID  string
	taskSvc service.TaskService
}

func (c *visibleTaskEventsClient) Send(msg []byte) error {
	env, err := common.Unmarshal[struct {
		TaskID string `json:"task_id"`
	}](msg)
	if err != nil || env.TaskID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), eventAuthzTimeout)
	defer cancel()
	if _, err := c.taskSvc.GetTask(ctx, env.TaskID, c.userID); err != nil {
		return nil
	}
	return c.Client.Send(msg)
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
// Requires an authenticated user (RequireAuth); only events for tasks the user may view are delivered.
func (h *WSHandler) HandleEventsWS(c fiber.Ctx) error {
	user := CurrentUser(c)
	if user == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	if h.taskSvc == nil {
		return CentralErrorHandler(c, common.ErrServiceUnavailable)
	}
	if !wsOriginAllowed(c.RequestCtx()) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "websocket origin not allowed"})
	}
	userID := user.ID
	err := wsUpgrader.Upgrade(c.RequestCtx(), func(conn *websocket.Conn) {
		clientID := common.NewID()
		client := &visibleTaskEventsClient{Client: ws.NewConnClient(clientID, "global", conn), userID: userID, taskSvc: h.taskSvc}
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
// It requires an authenticated user (RequireAuth) with RoleViewer+ on the task's team, checked
// before the upgrade; it fails closed when no task service is available.
func WebSocketLogsHandler(hub *ws.Hub, taskSvc service.TaskService, logSvc service.LogService) fiber.Handler {
	return func(c fiber.Ctx) error {
		user := CurrentUser(c)
		if user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
		}
		taskID := c.Params("task_id")
		if taskID == "" || strings.Contains(taskID, "/") {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "task_id is required"})
		}
		if taskSvc == nil {
			return CentralErrorHandler(c, common.ErrServiceUnavailable)
		}
		if !wsOriginAllowed(c.RequestCtx()) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "websocket origin not allowed"})
		}
		if _, err := taskSvc.GetTask(c.Context(), taskID, user.ID); err != nil {
			return CentralErrorHandler(c, err)
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
