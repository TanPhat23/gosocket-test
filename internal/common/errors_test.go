package common_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"agent-task-manager/internal/common"
)

func TestAppErrorErrorWithoutCause(t *testing.T) {
	appErr := common.NewAppError(404, "task not found", nil)
	if got := appErr.Error(); got != "task not found" {
		t.Fatalf("Error() = %q, want %q", got, "task not found")
	}
	if appErr.Unwrap() != nil {
		t.Fatalf("Unwrap() on error without cause must be nil")
	}
}

func TestAppErrorErrorIncludesCause(t *testing.T) {
	cause := errors.New("connection refused")
	appErr := common.NewAppError(503, "service unavailable", cause)

	want := "service unavailable: connection refused"
	if got := appErr.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if appErr.Unwrap() != cause {
		t.Fatalf("Unwrap() = %v, want original cause", appErr.Unwrap())
	}
}

func TestAppErrorIsMatchesSentinelThroughWrapping(t *testing.T) {
	appErr := common.NewAppError(403, "access denied", common.ErrForbidden)
	wrapped := fmt.Errorf("handler: %w", appErr)

	if !errors.Is(wrapped, common.ErrForbidden) {
		t.Fatalf("errors.Is must reach the sentinel cause through AppError")
	}
	if errors.Is(wrapped, common.ErrNotFound) {
		t.Fatalf("errors.Is must not match an unrelated sentinel")
	}
}

func TestAppErrorIsNotMatchedWhenCauseIsNil(t *testing.T) {
	appErr := common.NewAppError(400, "bad request", nil)
	if errors.Is(appErr, common.ErrNotFound) {
		t.Fatalf("AppError without cause must not match any sentinel")
	}
}

func TestAppErrorAsExtractsStatusCode(t *testing.T) {
	inner := common.NewAppError(404, "project not found", common.ErrNotFound)
	wrapped := fmt.Errorf("service: %w", inner)

	var target *common.AppError
	if !errors.As(wrapped, &target) {
		t.Fatalf("errors.As must find *AppError in the chain")
	}
	if target.Code != 404 || target.Message != "project not found" {
		t.Fatalf("unexpected extracted AppError: %+v", target)
	}
}

func TestAppErrorAsDoesNotMatchPlainErrors(t *testing.T) {
	var target *common.AppError
	if errors.As(common.ErrNotFound, &target) {
		t.Fatalf("plain sentinel must not be reported as *AppError")
	}
}

func TestAppErrorWrappingRuntimeQuarantine(t *testing.T) {
	appErr := common.NewAppError(503, "tasks are paused", common.ErrRuntimeQuarantined)
	if !errors.Is(appErr, common.ErrRuntimeQuarantined) {
		t.Fatalf("quarantine sentinel must remain detectable through AppError")
	}
}

func TestAppErrorJSONOmitsCause(t *testing.T) {
	appErr := common.NewAppError(500, "safe message", errors.New("secret internal detail"))

	data, err := json.Marshal(appErr)
	if err != nil {
		t.Fatalf("marshal AppError: %v", err)
	}
	if strings.Contains(string(data), "secret internal detail") {
		t.Fatalf("underlying cause leaked into JSON: %s", data)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal AppError JSON: %v", err)
	}
	if decoded["code"] != float64(500) || decoded["message"] != "safe message" {
		t.Fatalf("unexpected JSON: %s", data)
	}
}
