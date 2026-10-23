package httpx_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-task-manager/internal/integrations/httpx"
)

func TestDo_HeadersBodyAndEmptyHeaderSkipped(t *testing.T) {
	var got *http.Request
	var body string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		b, _ := io.ReadAll(r.Body)
		body = string(b)
	}))
	defer s.Close()

	resp, err := httpx.Do(context.Background(), s.Client(), http.MethodPost, s.URL+"/p", map[string]string{"a": "b"},
		map[string]string{"Accept": "x/y", "Authorization": ""}, "svc api")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got.Method != "POST" || got.URL.Path != "/p" || body != `{"a":"b"}` {
		t.Errorf("req: %s %s %q", got.Method, got.URL.Path, body)
	}
	if got.Header.Get("Accept") != "x/y" || got.Header.Get("Content-Type") != "application/json" {
		t.Errorf("headers: %v", got.Header)
	}
	if _, ok := got.Header["Authorization"]; ok {
		t.Errorf("empty Authorization must be skipped")
	}
}

func TestDo_NoBodyNoContentType(t *testing.T) {
	var ct string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ct = r.Header.Get("Content-Type") }))
	defer s.Close()
	resp, err := httpx.Do(context.Background(), s.Client(), http.MethodGet, s.URL, nil, nil, "svc api")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if ct != "" {
		t.Errorf("Content-Type=%q", ct)
	}
}

func TestDo_Errors(t *testing.T) {
	ctx := context.Background()
	if _, err := httpx.Do(ctx, http.DefaultClient, "GET", "http://x", make(chan int), nil, "svc api"); err == nil || !strings.HasPrefix(err.Error(), "failed to marshal request body: ") {
		t.Errorf("marshal: %v", err)
	}
	if _, err := httpx.Do(ctx, http.DefaultClient, "BAD METHOD", "http://x", nil, nil, "svc api"); err == nil || !strings.HasPrefix(err.Error(), "failed to create request: ") {
		t.Errorf("create: %v", err)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url, c := s.URL, s.Client()
	s.Close()
	_, err := httpx.Do(ctx, c, "GET", url, nil, nil, "svc api")
	if err == nil || !strings.HasPrefix(err.Error(), "svc api request failed: ") {
		t.Errorf("transport: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = httpx.Do(cctx, http.DefaultClient, "GET", "http://127.0.0.1:1", nil, nil, "svc api")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancel should be wrapped: %v", err)
	}
}

func TestCheckSuccess(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{200, ""}, {201, ""}, {299, ""},
		{300, "pfx 300: body"}, {404, "pfx 404: body"}, {500, "pfx 500: body"},
	} {
		resp := &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader("body"))}
		err := httpx.CheckSuccess(resp, "pfx")
		switch {
		case tc.want == "" && err != nil, tc.want != "" && (err == nil || err.Error() != tc.want):
			t.Errorf("status %d: got %v want %q", tc.status, err, tc.want)
		}
	}
	// empty body
	resp := &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(""))}
	if err := httpx.CheckSuccess(resp, "p"); err == nil || err.Error() != "p 500: " {
		t.Errorf("empty body: %v", err)
	}
}

func TestDecodeJSON(t *testing.T) {
	type item struct {
		N int `json:"n"`
	}
	v, err := httpx.DecodeJSON[[]item](strings.NewReader(`[{"n":1},{"n":2}]`), "items")
	if err != nil || len(v) != 2 || v[1].N != 2 {
		t.Fatalf("v=%v err=%v", v, err)
	}
	_, err = httpx.DecodeJSON[item](strings.NewReader(`{bad`), "item")
	if err == nil || !strings.HasPrefix(err.Error(), "failed to decode item: ") {
		t.Errorf("malformed: %v", err)
	}
	_, err = httpx.DecodeJSON[item](strings.NewReader(``), "item")
	if err == nil || err.Error() != "failed to decode item: EOF" || !errors.Is(err, io.EOF) {
		t.Errorf("empty: %v", err)
	}
}
