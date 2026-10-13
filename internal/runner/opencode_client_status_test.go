package runner_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/runner"
)

const statusBodyMarker = "LEAKED_BODY_MARKER_xyz"

type statusExpectation struct {
	code     int
	sentinel error  // matched with errors.Is when non-nil
	exact    string // exact error string when non-empty
	wantNil  bool
}

// expectation helpers
func generic(code int, text string) statusExpectation {
	return statusExpectation{code: code, exact: text}
}

func standardMapping(suffix string) []statusExpectation {
	mk := func(code int, s string) statusExpectation {
		return generic(code, "opencode server returned status "+s+suffix)
	}
	return []statusExpectation{
		{code: 404, sentinel: common.ErrNotFound},
		{code: 500, sentinel: common.ErrServiceUnavailable},
		{code: 503, sentinel: common.ErrServiceUnavailable},
		mk(400, "400"),
		mk(401, "401"),
		mk(403, "403"),
		mk(409, "409"),
	}
}

func runStatusCases(t *testing.T, okCodes []int, okBody string, cases []statusExpectation, call func(c interface{}) error, build func(url string) interface{}) {
	t.Helper()
	for _, code := range okCodes {
		code := code
		t.Run("accept_"+http.StatusText(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				if code != http.StatusNoContent {
					_, _ = w.Write([]byte(okBody))
				}
			}))
			defer srv.Close()
			if err := call(build(srv.URL)); err != nil {
				t.Fatalf("status %d: expected nil error, got %v", code, err)
			}
		})
	}
	for _, tc := range cases {
		tc := tc
		t.Run(http.StatusText(tc.code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(statusBodyMarker))
			}))
			defer srv.Close()
			err := call(build(srv.URL))
			switch {
			case tc.wantNil:
				if err != nil {
					t.Fatalf("expected nil, got %v", err)
				}
				return
			case tc.sentinel != nil:
				if !errors.Is(err, tc.sentinel) {
					t.Fatalf("expected %v, got %v", tc.sentinel, err)
				}
			default:
				if err == nil || err.Error() != tc.exact {
					t.Fatalf("expected %q, got %v", tc.exact, err)
				}
			}
			if err != nil && strings.Contains(err.Error(), statusBodyMarker) {
				t.Fatalf("response body leaked into error: %v", err)
			}
		})
	}
}

func newOC(u string) interface{} { return runner.NewOpenCodeClient(u, "pw", nil) }

func TestOpenCodeClient_StatusMapping_GetSession(t *testing.T) {
	runStatusCases(t, []int{200}, `{"data":{"id":"ses_1"}}`, standardMapping(""),
		func(c interface{}) error {
			_, err := c.(runner.OpenCodeClient).GetSession(context.Background(), "ses_1")
			return err
		}, newOC)
}

func TestOpenCodeClient_StatusMapping_GetSessionMessages(t *testing.T) {
	runStatusCases(t, []int{200}, `[]`, standardMapping(""),
		func(c interface{}) error {
			_, err := c.(runner.OpenCodeClient).GetSessionMessages(context.Background(), "ses_1")
			return err
		}, newOC)
}

func TestOpenCodeClient_StatusMapping_CompactSession(t *testing.T) {
	runStatusCases(t, []int{200, 201, 204}, ``, standardMapping(""),
		func(c interface{}) error {
			return c.(runner.OpenCodeClient).CompactSession(context.Background(), "ses_1")
		}, newOC)
}

func TestOpenCodeClient_StatusMapping_InterruptSession(t *testing.T) {
	cases := standardMapping("")
	cases[0] = statusExpectation{code: 404, wantNil: true} // 404 is tolerated
	runStatusCases(t, []int{200, 204}, ``, cases,
		func(c interface{}) error {
			return c.(runner.OpenCodeClient).InterruptSession(context.Background(), "ses_1")
		}, newOC)
}

// ListChildSessions intentionally keeps a flat >=400 mapping: 404 and 5xx are
// NOT mapped to sentinels. Pinned so any future unification is deliberate.
func TestOpenCodeClient_StatusMapping_ListChildSessions_Flat(t *testing.T) {
	cases := []statusExpectation{
		generic(404, "opencode server returned status 404"),
		generic(500, "opencode server returned status 500"),
		generic(503, "opencode server returned status 503"),
		generic(400, "opencode server returned status 400"),
		generic(401, "opencode server returned status 401"),
		generic(403, "opencode server returned status 403"),
		generic(409, "opencode server returned status 409"),
	}
	runStatusCases(t, []int{200}, `{"data":[]}`, cases,
		func(c interface{}) error {
			_, err := c.(runner.OpenCodeClient).ListChildSessions(context.Background(), "ses_1")
			return err
		}, newOC)
}

// PromptSession appends the (trimmed) response body for 4xx. Pinned as-is; not
// part of the shared mapping. Only sentinel/5xx/empty-body behavior is checked here.
func TestOpenCodeClient_StatusMapping_PromptSession(t *testing.T) {
	c := func(code int, body string) error {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(body))
		}))
		defer srv.Close()
		return runner.NewOpenCodeClient(srv.URL, "pw", nil).PromptSession(context.Background(), "ses_1", "hi")
	}
	if err := c(200, ""); err != nil {
		t.Fatalf("200: %v", err)
	}
	if err := c(404, statusBodyMarker); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("404: %v", err)
	}
	for _, code := range []int{500, 503} {
		if err := c(code, statusBodyMarker); !errors.Is(err, common.ErrServiceUnavailable) || strings.Contains(err.Error(), statusBodyMarker) {
			t.Fatalf("%d: %v", code, err)
		}
	}
	if err := c(400, ""); err == nil || err.Error() != "opencode server returned status 400" {
		t.Fatalf("400 empty body: %v", err)
	}
	if err := c(409, "  conflict \n"); err == nil || err.Error() != "opencode server returned status 409: conflict" {
		t.Fatalf("409 with body: %v", err)
	}
}

// EnsureSessionAtLocation: create returns 400 (forces move path); move status is mapped.
func TestOpenCodeClient_StatusMapping_EnsureSessionMove(t *testing.T) {
	mk := func(moveCode int) error {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/session" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(moveCode)
			_, _ = w.Write([]byte(statusBodyMarker))
		}))
		defer srv.Close()
		return runner.NewOpenCodeClient(srv.URL, "pw", nil).EnsureSessionAtLocation(context.Background(), "ses_1", "t", "/ws")
	}
	for _, code := range []int{200, 204} {
		if err := mk(code); err != nil {
			t.Fatalf("move %d: %v", code, err)
		}
	}
	if err := mk(404); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("404: %v", err)
	}
	for _, code := range []int{500, 503} {
		if err := mk(code); !errors.Is(err, common.ErrServiceUnavailable) {
			t.Fatalf("%d: %v", code, err)
		}
	}
	for _, code := range []int{400, 401, 403, 409} {
		err := mk(code)
		if err == nil || !strings.HasPrefix(err.Error(), "opencode server returned status ") || !strings.HasSuffix(err.Error(), " on move") || strings.Contains(err.Error(), statusBodyMarker) {
			t.Fatalf("%d: %v", code, err)
		}
	}
	// exact strings
	if err := mk(409); err.Error() != "opencode server returned status 409 on move" {
		t.Fatalf("409 exact: %v", err)
	}
	// 201 (not accepted by move path) falls through to nil today
	if err := mk(201); err != nil {
		t.Fatalf("move 201: %v", err)
	}
}
