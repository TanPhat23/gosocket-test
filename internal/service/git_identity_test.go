package service_test

import (
	"strings"
	"testing"

	"agent-task-manager/internal/service"
)

func TestNormalizeGitIdentity(t *testing.T) {
	valid := []struct{ name, email, wantName, wantEmail string }{
		{"Jane Dev", "jane@example.com", "Jane Dev", "jane@example.com"},
		{"  Jane Dev ", " jane@example.com\n", "Jane Dev", "jane@example.com"},
		{"Jane O'Neil-Smith", "jane+atm@sub.example.co", "Jane O'Neil-Smith", "jane+atm@sub.example.co"},
		{"", "", "", ""},
		{"  ", "  ", "", ""},
	}
	for _, c := range valid {
		name, email, err := service.NormalizeGitIdentity(c.name, c.email)
		if err != nil || name != c.wantName || email != c.wantEmail {
			t.Errorf("%q/%q: got %q/%q err %v, want %q/%q", c.name, c.email, name, email, err, c.wantName, c.wantEmail)
		}
	}

	invalid := map[string][2]string{
		"name only":        {"Jane", ""},
		"email only":       {"", "jane@example.com"},
		"name too long":    {strings.Repeat("a", 101), "jane@example.com"},
		"angle in name":    {"Jane <x@y.z>", "jane@example.com"},
		"newline in name":  {"Jane\nDev", "jane@example.com"},
		"tab in name":      {"Jane\tDev", "jane@example.com"},
		"not an address":   {"Jane", "jane"},
		"display form":     {"Jane", "Jane <jane@example.com>"},
		"space in email":   {"Jane", "jane @example.com"},
		"newline in email": {"Jane", "jane@example.com\nx"},
		"email too long":   {"Jane", strings.Repeat("a", 250) + "@b.co"},
		"two addresses":    {"Jane", "a@b.co, c@d.co"},
	}
	for label, c := range invalid {
		if _, _, err := service.NormalizeGitIdentity(c[0], c[1]); err == nil {
			t.Errorf("%s: %q/%q was accepted", label, c[0], c[1])
		}
	}
}
