package api

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"agent-task-manager/internal/auth"
	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/service"
	"agent-task-manager/internal/ws"
	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

// eventAuthzTimeout bounds the per-event visibility lookup for /ws/events subscribers and each
// periodic re-validation of an open socket.
const eventAuthzTimeout = 5 * time.Second

// eventAuthzCacheTTL is how long a /ws/events subscriber remembers that a task is, or is not, visible to
// it. It matches the revalidation interval, so a revoked membership stops delivering events within
// roughly one interval either way.
const eventAuthzCacheTTL = 30 * time.Second

// eventAuthzCacheMax bounds the per-subscriber cache; expired entries are dropped when it is reached.
const eventAuthzCacheMax = 1024

// defaultRevalidateInterval is how often open sockets re-check their credentials and access.
const defaultRevalidateInterval = 30 * time.Second

// errWSAccessRevoked reports that an open socket's credentials no longer authenticate its user.
var errWSAccessRevoked = errors.New("websocket access revoked")

// visibleTaskEventsClient wraps a global events subscriber and forwards only messages about tasks
// the subscribing user may view (RoleViewer+ on the task's team). Messages without a task ID or that
// cannot be authorized are dropped (fail closed). Send runs in its own goroutine per broadcast.
type visibleTaskEventsClient struct {
	client  ws.Client
	userID  string
	token   *models.APIToken // the PAT that authenticated the socket, if any; limits visibility to its team
	taskSvc service.TaskService
	now     func() time.Time // clock for the cache; nil means time.Now

	mu    sync.Mutex
	cache map[string]eventAuthzEntry
}

type eventAuthzEntry struct {
	visible bool
	expires time.Time
}

// ID, TaskID and Close make visibleTaskEventsClient a ws.Client. Send is overridden below.
func (c *visibleTaskEventsClient) ID() string     { return c.client.ID() }
func (c *visibleTaskEventsClient) TaskID() string { return c.client.TaskID() }
func (c *visibleTaskEventsClient) Close() error   { return c.client.Close() }

func (c *visibleTaskEventsClient) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// visible reports whether the subscriber may see taskID's events. Allowed and forbidden answers are
// cached for eventAuthzCacheTTL; any other lookup error is treated as not visible but never cached.
func (c *visibleTaskEventsClient) visible(taskID string) bool {
	now := c.clock()
	c.mu.Lock()
	e, ok := c.cache[taskID]
	c.mu.Unlock()
	if ok && now.Before(e.expires) {
		return e.visible
	}

	ctx, cancel := context.WithTimeout(withTokenScope(context.Background(), c.token), eventAuthzTimeout)
	defer cancel()
	_, err := c.taskSvc.GetTask(ctx, taskID, c.userID)
	if err != nil && !errors.Is(err, common.ErrForbidden) {
		return false
	}
	visible := err == nil

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = make(map[string]eventAuthzEntry)
	}
	if len(c.cache) >= eventAuthzCacheMax {
		for k, v := range c.cache {
			if !now.Before(v.expires) {
				delete(c.cache, k)
			}
		}
		if len(c.cache) >= eventAuthzCacheMax {
			return visible // still full of live entries: answer without remembering
		}
	}
	c.cache[taskID] = eventAuthzEntry{visible: visible, expires: now.Add(eventAuthzCacheTTL)}
	return visible
}

func (c *visibleTaskEventsClient) Send(msg []byte) error {
	env, err := common.Unmarshal[struct {
		TaskID string `json:"task_id"`
	}](msg)
	if err != nil || env.TaskID == "" {
		return nil
	}
	if !c.visible(env.TaskID) {
		return nil
	}
	return c.client.Send(msg)
}

// WSHandler handles WebSocket connections for terminal and log streaming.
type WSHandler struct {
	hub                *ws.Hub
	taskSvc            service.TaskService
	logSvc             service.LogService
	revalidateInterval time.Duration
	sessionPoll        time.Duration
	upgrader           websocket.FastHTTPUpgrader

	// Set by NewServer: the services RequireAuth uses, for re-validating open sockets, and the
	// configured browser origins trusted in addition to the API's own host.
	authService    service.AuthService
	sessions       *auth.SessionManager
	allowedOrigins []string
}

// NewWSHandler creates a new WSHandler.
func NewWSHandler(hub *ws.Hub, opts ...func(*WSHandler)) *WSHandler {
	h := &WSHandler{hub: hub, revalidateInterval: defaultRevalidateInterval, sessionPoll: defaultSessionPoll}
	h.upgrader = websocket.FastHTTPUpgrader{CheckOrigin: h.originAllowed}
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

// defaultSessionPoll is how often the session relay re-reads the project's OpenCode session.
const defaultSessionPoll = 1500 * time.Millisecond

// WithWSSessionPoll sets the session relay's poll interval. Non-positive values keep the default.
func WithWSSessionPoll(d time.Duration) func(*WSHandler) {
	return func(h *WSHandler) {
		if d > 0 {
			h.sessionPoll = d
		}
	}
}

// WithWSRevalidateInterval sets how often open sockets re-check their credentials and access
// (default 30s). Non-positive values keep the default.
func WithWSRevalidateInterval(d time.Duration) func(*WSHandler) {
	return func(h *WSHandler) {
		if d > 0 {
			h.revalidateInterval = d
		}
	}
}

// originAllowed rejects cross-site WebSocket handshakes: a browser Origin must name the host[:port]
// the request was sent to, or one of the configured allowed origins. Requests without an Origin
// header come from non-browser clients, which cannot be driven cross-site; they still need
// RequireAuth credentials.
func (h *WSHandler) originAllowed(ctx *fasthttp.RequestCtx) bool {
	origin := ctx.Request.Header.Peek(fasthttp.HeaderOrigin)
	if len(origin) == 0 {
		return true
	}
	return originAllowed(string(origin), string(ctx.Host()), h.allowedOrigins)
}

// reauthorize re-runs authentication with the credentials captured at the handshake and requires
// that they still resolve to userID (the user exists, the session has not expired, the PAT has not
// been deleted or expired) through the same credential that authorized the handshake (the session
// cookie, or the PAT with handshakeToken's ID), so an expired cookie cannot hand the socket over to
// a PAT sent alongside it. It returns ctx restricted to the PAT's team scope, if a PAT was used.
func (h *WSHandler) reauthorize(ctx context.Context, creds requestCredentials, userID string, handshakeToken *models.APIToken) (context.Context, error) {
	user, token, ok := creds.authenticate(ctx, h.authService, h.sessions)
	if !ok || user.ID != userID || !sameToken(token, handshakeToken) {
		return nil, errWSAccessRevoked
	}
	return withTokenScope(ctx, token), nil
}

// sameToken reports whether a and b are both absent or are the same API token.
func sameToken(a, b *models.APIToken) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ID == b.ID
}

// closeOnRevocation runs check every revalidateInterval until done is closed. On the first failure
// it sends a policy-violation (1008) close frame and closes conn, which ends the handler's read
// loop. Any check error, including a transient lookup failure, closes the socket (fail closed).
func (h *WSHandler) closeOnRevocation(conn *websocket.Conn, done <-chan struct{}, check func(context.Context) error) {
	ticker := time.NewTicker(h.revalidateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), eventAuthzTimeout)
			err := check(ctx)
			cancel()
			if err != nil {
				msg := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "access revoked")
				_ = conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))
				_ = conn.Close()
				return
			}
		}
	}
}

// HandleEventsWS upgrades connection to WebSocket for streaming all task status changes in real-time.
// Requires an authenticated user (RequireAuth); only events for tasks the user may view are delivered,
// and the socket is closed (1008) once the user's credentials stop authenticating.
func (h *WSHandler) HandleEventsWS(c fiber.Ctx) error {
	user := CurrentUser(c)
	if user == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	if h.taskSvc == nil || h.authService == nil {
		return CentralErrorHandler(c, common.ErrServiceUnavailable)
	}
	if !h.originAllowed(c.RequestCtx()) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "websocket origin not allowed"})
	}
	userID := user.ID
	token := CurrentAPIToken(c)
	creds := credentialsFrom(c)
	err := h.upgrader.Upgrade(c.RequestCtx(), func(conn *websocket.Conn) {
		done := make(chan struct{})
		defer close(done)
		go h.closeOnRevocation(conn, done, func(ctx context.Context) error {
			_, err := h.reauthorize(ctx, creds, userID, token)
			return err
		})

		clientID := common.NewID()
		client := &visibleTaskEventsClient{client: ws.NewConnClient(clientID, "global", conn), userID: userID, token: token, taskSvc: h.taskSvc}
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

// HandleTerminalWS streams real-time terminal ANSI logs and task status events over WebSocket.
// It requires an authenticated user (RequireAuth) with RoleViewer+ on the task's team, checked
// before the upgrade and again every revalidateInterval (closing with 1008 once it fails); it
// fails closed when no task or auth service is available.
func (h *WSHandler) HandleTerminalWS(c fiber.Ctx) error {
	user := CurrentUser(c)
	if user == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	taskID := c.Params("task_id")
	if taskID == "" || strings.Contains(taskID, "/") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "task_id is required"})
	}
	if h.taskSvc == nil || h.authService == nil {
		return CentralErrorHandler(c, common.ErrServiceUnavailable)
	}
	if !h.originAllowed(c.RequestCtx()) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "websocket origin not allowed"})
	}
	if _, err := h.taskSvc.GetTask(c.Context(), taskID, user.ID); err != nil {
		return CentralErrorHandler(c, err)
	}

	taskID = strings.Clone(taskID)
	userID := user.ID
	token := CurrentAPIToken(c)
	creds := credentialsFrom(c)
	err := h.upgrader.Upgrade(c.RequestCtx(), func(conn *websocket.Conn) {
		done := make(chan struct{})
		defer close(done)
		go h.closeOnRevocation(conn, done, func(ctx context.Context) error {
			scoped, err := h.reauthorize(ctx, creds, userID, token)
			if err != nil {
				return err
			}
			_, err = h.taskSvc.GetTask(scoped, taskID, userID)
			return err
		})

		clientID := common.NewID()
		client := ws.NewConnClient(clientID, taskID, conn)
		h.hub.Register(client)
		defer func() {
			h.hub.Unregister(client)
			_ = client.Close()
		}()

		// Replay existing logs to newly connected client
		if h.logSvc != nil {
			logs, err := h.logSvc.GetLogs(context.Background(), taskID)
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

// HandleSessionWS relays a task's OpenCode session (transcript, status, subagents) to the browser. It is a
// server-side poll of the same authorized read as GET /tasks/:id/session, pushed over the socket only when the
// snapshot changed; it is not a native OpenCode event stream. Authorization is the service's per-task Viewer
// check on every read (so it is re-checked each tick), plus the periodic credential revalidation.
func (h *WSHandler) HandleSessionWS(c fiber.Ctx) error {
	user := CurrentUser(c)
	if user == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	taskID := c.Params("task_id")
	if taskID == "" || strings.Contains(taskID, "/") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "task_id is required"})
	}
	if h.taskSvc == nil || h.authService == nil {
		return CentralErrorHandler(c, common.ErrServiceUnavailable)
	}
	if !h.originAllowed(c.RequestCtx()) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "websocket origin not allowed"})
	}
	if _, err := h.taskSvc.GetTask(c.Context(), taskID, user.ID); err != nil {
		return CentralErrorHandler(c, err)
	}

	taskID = strings.Clone(taskID)
	userID := user.ID
	token := CurrentAPIToken(c)
	creds := credentialsFrom(c)
	poll := h.sessionPoll
	err := h.upgrader.Upgrade(c.RequestCtx(), func(conn *websocket.Conn) {
		done := make(chan struct{})
		defer close(done)
		go h.closeOnRevocation(conn, done, func(ctx context.Context) error {
			scoped, err := h.reauthorize(ctx, creds, userID, token)
			if err != nil {
				return err
			}
			_, err = h.taskSvc.GetTask(scoped, taskID, userID)
			return err
		})

		closed := make(chan struct{})
		go func() {
			defer close(closed)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		var last []byte
		ticker := time.NewTicker(poll)
		defer ticker.Stop()
		for {
			ctx, cancel := context.WithTimeout(context.Background(), eventAuthzTimeout)
			sess, err := h.taskSvc.GetTaskSession(ctx, taskID, userID)
			cancel()
			var out []byte
			if err != nil {
				if errors.Is(err, common.ErrForbidden) || errors.Is(err, common.ErrNotFound) {
					msg := websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "access revoked")
					_ = conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))
					_ = conn.Close()
					return
				}
				out, _ = common.Marshal(map[string]any{"type": "error", "error": err.Error()})
			} else {
				out, _ = common.Marshal(map[string]any{"type": "session", "session": sess})
			}
			if out != nil && !bytes.Equal(out, last) {
				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if werr := conn.WriteMessage(websocket.TextMessage, out); werr != nil {
					return
				}
				last = out
			}
			select {
			case <-closed:
				return
			case <-ticker.C:
			}
		}
	})
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "websocket upgrade failed"})
	}
	return nil
}
