package api

import (
	"context"
	"encoding/json"

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

// WebSocketLogsHandler streams real-time terminal ANSI logs and task status events over WebSocket.
func WebSocketLogsHandler(hub *ws.Hub, taskSvc service.TaskService, logSvc service.LogService) fiber.Handler {
	return func(c fiber.Ctx) error {
		taskID := c.Params("task_id")
		if taskID == "" {
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
						data, _ := json.Marshal(msg)
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
