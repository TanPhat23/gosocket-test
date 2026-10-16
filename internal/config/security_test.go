package config_test

import (
	"reflect"
	"testing"

	"agent-task-manager/internal/config"
)

func TestLoadSecurityConfig_AllowedOrigins(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{name: "unset", raw: "", want: nil},
		{name: "blank entries ignored", raw: " , ,", want: nil},
		{name: "single origin", raw: "https://app.example.com", want: []string{"https://app.example.com"}},
		{name: "list normalized to lowercase and trimmed", raw: " HTTPS://App.Example.com , http://LOCALHOST:5173 ",
			want: []string{"https://app.example.com", "http://localhost:5173"}},
		{name: "trailing slash is a path", raw: "https://app.example.com/", wantErr: true},
		{name: "path", raw: "https://app.example.com/ui", wantErr: true},
		{name: "query", raw: "https://app.example.com?x=1", wantErr: true},
		{name: "fragment", raw: "https://app.example.com#x", wantErr: true},
		{name: "userinfo", raw: "https://user:pw@app.example.com", wantErr: true},
		{name: "missing scheme", raw: "app.example.com", wantErr: true},
		{name: "non-http scheme", raw: "ftp://app.example.com", wantErr: true},
		{name: "missing host", raw: "https://", wantErr: true},
		{name: "wildcard", raw: "*", wantErr: true},
		{name: "null origin", raw: "null", wantErr: true},
		{name: "one bad entry fails the list", raw: "https://ok.example.com,https://bad.example.com/x", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ATM_ALLOWED_ORIGINS", tc.raw)
			t.Setenv("ATM_ENABLE_LOCAL_LOGIN", "")
			got, err := config.LoadSecurityConfig("development")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %+v", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.raw, err)
			}
			if !reflect.DeepEqual(got.AllowedOrigins, tc.want) {
				t.Errorf("AllowedOrigins = %#v, want %#v", got.AllowedOrigins, tc.want)
			}
		})
	}
}

func TestLoadSecurityConfig_EnableLocalLogin(t *testing.T) {
	cases := []struct {
		name    string
		appEnv  string
		raw     string
		want    bool
		wantErr bool
	}{
		{name: "unset in development is disabled", appEnv: "development", raw: "", want: false},
		{name: "explicit true in development", appEnv: "development", raw: "true", want: true},
		{name: "explicit false in development", appEnv: "development", raw: "false", want: false},
		{name: "empty app env is not production", appEnv: "", raw: "true", want: true},
		{name: "unset in production is disabled", appEnv: "production", raw: "", want: false},
		{name: "false in production is disabled", appEnv: "production", raw: "false", want: false},
		{name: "true in production is a loud error", appEnv: "production", raw: "true", wantErr: true},
		{name: "garbage value is an error", appEnv: "development", raw: "yes-please", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ATM_ALLOWED_ORIGINS", "")
			t.Setenv("ATM_ENABLE_LOCAL_LOGIN", tc.raw)
			got, err := config.LoadSecurityConfig(tc.appEnv)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				if got.EnableLocalLogin {
					t.Fatal("local login must be disabled when an error is returned")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.EnableLocalLogin != tc.want {
				t.Errorf("EnableLocalLogin = %v, want %v", got.EnableLocalLogin, tc.want)
			}
		})
	}
}
