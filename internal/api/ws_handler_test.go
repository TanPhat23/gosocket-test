package api_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/common"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/ws"
	"github.com/gofiber/fiber/v3"
)

type mockLogService struct {
	logs map[string][]models.TaskLog
}

func (m *mockLogService) AppendLog(ctx context.Context, taskID string, attemptID *string, chunk string) error {
	m.logs[taskID] = append(m.logs[taskID], models.TaskLog{
		TaskID:    taskID,
		AttemptID: attemptID,
		LogChunk:  chunk,
		Timestamp: time.Now().UTC(),
	})
	return nil
}

func (m *mockLogService) GetLogs(ctx context.Context, taskID string) ([]models.TaskLog, error) {
	return m.logs[taskID], nil
}

func TestWebSocketLogsHandler_Upgrade(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	logSvc := &mockLogService{
		logs: map[string][]models.TaskLog{
			"task-123": {
				{TaskID: "task-123", LogChunk: "Build started...\n", Timestamp: time.Now().UTC()},
			},
		},
	}

	app := fiber.New()
	app.Get("/api/v1/ws/tasks/:task_id/logs", api.WebSocketLogsHandler(hub, nil, logSvc))

	// Normal non-websocket request should return 400 Bad Request
	req := httptest.NewRequest("GET", "/api/v1/ws/tasks/task-123/logs", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for non-websocket upgrade request, got %d", resp.StatusCode)
	}
}

func TestWebSocketClientMockAndHub(t *testing.T) {
	hub := ws.NewHub()
	go hub.Run()
	defer hub.Stop()

	taskID := common.NewID()
	client := ws.NewMockClient("client-test", taskID)
	hub.Register(client)
	time.Sleep(15 * time.Millisecond)

	err := hub.BroadcastTaskLog(taskID, "Agent compiled successfully")
	if err != nil {
		t.Fatalf("BroadcastTaskLog failed: %v", err)
	}

	select {
	case msg := <-client.Messages():
		if len(msg) == 0 {
			t.Errorf("expected non-empty message")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for broadcast message")
	}
}
