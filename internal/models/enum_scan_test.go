package models_test

import (
	"testing"

	"agent-task-manager/internal/models"
)

type scanner interface{ Scan(any) error }

func TestStringEnumScanParity(t *testing.T) {
	cases := []struct {
		name  string
		new   func() scanner
		valid string
	}{
		{"IntegrationProvider", func() scanner { return new(models.IntegrationProvider) }, "jira"},
		{"CredentialProvider", func() scanner { return new(models.CredentialProvider) }, "github"},
		{"TaskStatus", func() scanner { return new(models.TaskStatus) }, "in_progress"},
		{"AssigneeType", func() scanner { return new(models.AssigneeType) }, "agent"},
		{"AttemptStatus", func() scanner { return new(models.AttemptStatus) }, "running"},
		{"WebhookStatus", func() scanner { return new(models.WebhookStatus) }, "received"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := func(in any, want string) {
				t.Helper()
				err := tc.new().Scan(in)
				if want == "" {
					if err != nil {
						t.Fatalf("Scan(%#v) unexpected error: %v", in, err)
					}
					return
				}
				if err == nil || err.Error() != want {
					t.Fatalf("Scan(%#v) error = %v, want %q", in, err, want)
				}
			}
			check(nil, "cannot scan nil into "+tc.name)
			check(tc.valid, "")
			check([]byte(tc.valid), "")
			check("bogus", "invalid "+tc.name+" in database: \"bogus\"")
			check([]byte("bogus"), "invalid "+tc.name+" in database: \"bogus\"")
			check("", "invalid "+tc.name+" in database: \"\"")
			check(42, "cannot scan non-string value into "+tc.name)
			check(true, "cannot scan non-string value into "+tc.name)
		})
	}
}

func TestStringEnumScanAssignsOnlyOnSuccess(t *testing.T) {
	s := models.TaskStatusDone
	if err := s.Scan("bogus"); err == nil || s != models.TaskStatusDone {
		t.Fatalf("failed scan must not modify value, got %q err=%v", s, err)
	}
	if err := s.Scan([]byte("queued")); err != nil || s != models.TaskStatusQueued {
		t.Fatalf("got %q err=%v", s, err)
	}
}
