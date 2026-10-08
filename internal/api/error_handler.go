package api

import (
	"errors"
	"log/slog"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/service"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// CentralErrorHandler processes all errors returned by routes and middlewares in Fiber v3.
func CentralErrorHandler(c fiber.Ctx, err error) error {
	if err == nil {
		return nil
	}

	statusCode := fiber.StatusInternalServerError
	clientMessage := "Internal server error"

	// 1. Check for custom typed AppError using modern errors.AsType
	if appErr, ok := errors.AsType[*common.AppError](err); ok {
		statusCode = appErr.Code
		clientMessage = appErr.Message

	// 2. Check for native Fiber framework errors (e.g. 404 Route Not Found, 405 Method Not Allowed)
	} else if fibErr, ok := errors.AsType[*fiber.Error](err); ok {
		statusCode = fibErr.Code
		clientMessage = fibErr.Message

	// 3. Check for Domain / Sentinel Errors using errors.Is
	} else {
		switch {
		// 400 Bad Request
		case errors.Is(err, service.ErrInvalidWebhookPayload),
			errors.Is(err, service.ErrMissingDeliveryID):
			statusCode = fiber.StatusBadRequest
			clientMessage = err.Error()

		// 401 Unauthorized
		case errors.Is(err, service.ErrIntegrationNotFound),
			errors.Is(err, service.ErrInvalidSignature):
			statusCode = fiber.StatusUnauthorized
			clientMessage = "unauthorized"

		// 403 Forbidden
		case errors.Is(err, common.ErrForbidden):
			statusCode = fiber.StatusForbidden
			clientMessage = "Forbidden: insufficient permissions"

		// 404 Not Found
		case errors.Is(err, common.ErrNotFound),
			errors.Is(err, gorm.ErrRecordNotFound):
			statusCode = fiber.StatusNotFound
			clientMessage = "resource not found"

		// 503 Service Unavailable
		case errors.Is(err, common.ErrServiceUnavailable):
			statusCode = fiber.StatusServiceUnavailable
			clientMessage = common.ErrServiceUnavailable.Error()

		// 500 Internal Server Error (Sanitized fallback)
		default:
			slog.Error("Unhandled internal server error",
				"path", c.Path(),
				"method", c.Method(),
				"error", err,
			)
			statusCode = fiber.StatusInternalServerError
			clientMessage = "Internal server error"
		}
	}

	return c.Status(statusCode).JSON(fiber.Map{
		"error": clientMessage,
	})
}
