package api_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"agent-task-manager/internal/api"
	"agent-task-manager/internal/common"
	"agent-task-manager/internal/service"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func TestCentralErrorHandler(t *testing.T) {
	tests := []struct {
		name           string
		handlerErr     error
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "custom AppError",
			handlerErr:     common.NewAppError(fiber.StatusTeapot, "I am a teapot", errors.New("underlying")),
			expectedStatus: fiber.StatusTeapot,
			expectedBody:   `{"error":"I am a teapot"}`,
		},
		{
			name:           "native fiber.Error 404",
			handlerErr:     fiber.ErrNotFound,
			expectedStatus: fiber.StatusNotFound,
			expectedBody:   `{"error":"Not Found"}`,
		},
		{
			name:           "sentinel ErrInvalidWebhookPayload 400",
			handlerErr:     service.ErrInvalidWebhookPayload,
			expectedStatus: fiber.StatusBadRequest,
			expectedBody:   `{"error":"invalid webhook payload"}`,
		},
		{
			name:           "sentinel ErrMissingDeliveryID 400",
			handlerErr:     service.ErrMissingDeliveryID,
			expectedStatus: fiber.StatusBadRequest,
			expectedBody:   `{"error":"missing webhook delivery id"}`,
		},
		{
			name:           "sentinel ErrInvalidSignature 401",
			handlerErr:     service.ErrInvalidSignature,
			expectedStatus: fiber.StatusUnauthorized,
			expectedBody:   `{"error":"unauthorized"}`,
		},
		{
			name:           "sentinel ErrIntegrationNotFound 401",
			handlerErr:     service.ErrIntegrationNotFound,
			expectedStatus: fiber.StatusUnauthorized,
			expectedBody:   `{"error":"unauthorized"}`,
		},
		{
			name:           "sentinel ErrForbidden 403",
			handlerErr:     common.ErrForbidden,
			expectedStatus: fiber.StatusForbidden,
			expectedBody:   `{"error":"Forbidden: insufficient permissions"}`,
		},
		{
			name:           "sentinel ErrNotFound 404",
			handlerErr:     common.ErrNotFound,
			expectedStatus: fiber.StatusNotFound,
			expectedBody:   `{"error":"resource not found"}`,
		},
		{
			name:           "sentinel gorm.ErrRecordNotFound 404",
			handlerErr:     gorm.ErrRecordNotFound,
			expectedStatus: fiber.StatusNotFound,
			expectedBody:   `{"error":"resource not found"}`,
		},
		{
			name:           "sentinel ErrServiceUnavailable 503",
			handlerErr:     common.ErrServiceUnavailable,
			expectedStatus: fiber.StatusServiceUnavailable,
			expectedBody:   `{"error":"Agent execution service unavailable"}`,
		},
		{
			name:           "unknown internal error 500 masks internal detail",
			handlerErr:     errors.New("FATAL: password authentication failed for user postgres"),
			expectedStatus: fiber.StatusInternalServerError,
			expectedBody:   `{"error":"Internal server error"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New(fiber.Config{
				ErrorHandler: api.CentralErrorHandler,
			})

			app.Get("/test", func(c fiber.Ctx) error {
				return tt.handlerErr
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("unexpected app.Test error: %v", err)
			}

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			body, _ := io.ReadAll(resp.Body)
			if string(body) != tt.expectedBody {
				t.Errorf("expected body %s, got %s", tt.expectedBody, string(body))
			}
		})
	}
}
