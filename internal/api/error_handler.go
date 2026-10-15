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
			errors.Is(err, service.ErrMissingDeliveryID),
			errors.Is(err, service.ErrInvalidRepoSlug),
			errors.Is(err, service.ErrInvalidBaseBranch),
			errors.Is(err, service.ErrInvalidWebhookCallbackURL),
			errors.Is(err, service.ErrInvalidProvider),
			errors.Is(err, service.ErrInvalidState),
			errors.Is(err, service.ErrStateMismatch):
			statusCode = fiber.StatusBadRequest
			clientMessage = err.Error()

		// 401 Unauthorized
		case errors.Is(err, service.ErrInvalidCredentials):
			statusCode = fiber.StatusUnauthorized
			clientMessage = "Invalid email or password"
		case errors.Is(err, service.ErrInvalidToken):
			statusCode = fiber.StatusUnauthorized
			clientMessage = "invalid or expired token"
		case errors.Is(err, service.ErrInvalidSignature):
			statusCode = fiber.StatusUnauthorized
			clientMessage = "unauthorized"
		case errors.Is(err, service.ErrTokenExchange):
			statusCode = fiber.StatusUnauthorized
			clientMessage = "Failed to exchange authorization code"
		case errors.Is(err, service.ErrMissingIDToken):
			statusCode = fiber.StatusUnauthorized
			clientMessage = "Missing id_token in token response"
		case errors.Is(err, service.ErrVerifyIDToken):
			statusCode = fiber.StatusUnauthorized
			clientMessage = "Failed to verify ID token"
		case errors.Is(err, service.ErrNonceMismatch):
			statusCode = fiber.StatusUnauthorized
			clientMessage = "Nonce mismatch"
		case err.Error() == "invalid or expired github token":
			statusCode = fiber.StatusUnauthorized
			clientMessage = err.Error()

		// 429 Too Many Requests
		case err.Error() == "github api rate limit exceeded":
			statusCode = fiber.StatusTooManyRequests
			clientMessage = err.Error()

		// 403 Forbidden
		case errors.Is(err, common.ErrForbidden),
			errors.Is(err, service.ErrForbidden):
			statusCode = fiber.StatusForbidden
			clientMessage = "Forbidden: insufficient permissions"
		case errors.Is(err, service.ErrNotTeamMember),
			errors.Is(err, service.ErrUnauthorizedTenant):
			statusCode = fiber.StatusForbidden
			clientMessage = err.Error()

		// 404 Not Found
		case errors.Is(err, common.ErrNotFound),
			errors.Is(err, gorm.ErrRecordNotFound):
			statusCode = fiber.StatusNotFound
			clientMessage = "resource not found"
		case errors.Is(err, service.ErrProjectNotFound),
			errors.Is(err, service.ErrCredentialNotFound),
			errors.Is(err, service.ErrIntegrationNotFound):
			statusCode = fiber.StatusNotFound
			clientMessage = err.Error()

		// 503 Service Unavailable
		case errors.Is(err, common.ErrRuntimeQuarantined):
			statusCode = fiber.StatusServiceUnavailable
			clientMessage = "Task execution is temporarily unavailable"
		case errors.Is(err, common.ErrServiceUnavailable):
			statusCode = fiber.StatusServiceUnavailable
			clientMessage = common.ErrServiceUnavailable.Error()
		case errors.Is(err, service.ErrOIDCNotConfigured):
			statusCode = fiber.StatusServiceUnavailable
			clientMessage = service.ErrOIDCNotConfigured.Error()

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
