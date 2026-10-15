package git

import "testing"

func TestRemoteURL(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"slug", "owner/repo", "https://github.com/owner/repo.git"},
		{"https", "https://example.com/a/b.git", "https://example.com/a/b.git"},
		{"http", "http://example.com/a/b.git", "http://example.com/a/b.git"},
		{"file", "file:///tmp/repo", "file:///tmp/repo"},
		{"empty", "", "https://github.com/.git"},
		{"prefix not at start", "x/https://a", "https://github.com/x/https://a.git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RemoteURL(tt.in); got != tt.want {
				t.Fatalf("RemoteURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
