package trello_test

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"testing"

	"agent-task-manager/internal/integrations/trello"
)

func trelloSig(secret, body, cb string) string {
	m := hmac.New(sha1.New, []byte(secret))
	m.Write([]byte(body + cb))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

func TestVerifyWebhookSignature(t *testing.T) {
	const secret, body, cb = "app-secret", `{"action":{"id":"a1"}}`, "https://atm.example/api/v1/webhooks/trello"
	good := trelloSig(secret, body, cb)
	cases := []struct {
		name                  string
		secret, body, cb, sig string
		want                  bool
	}{
		{"valid", secret, body, cb, good, true},
		{"empty secret", "", body, cb, trelloSig("", body, cb), false},
		{"empty signature", secret, body, cb, "", false},
		{"empty callback url", secret, body, "", trelloSig(secret, body, ""), false},
		{"tampered body", secret, body + " ", cb, good, false},
		{"wrong callback url", secret, body, cb + "x", good, false},
		{"wrong secret", "other", body, cb, good, false},
		{"not base64", secret, body, cb, "!!!", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := trello.VerifyWebhookSignature(tc.secret, []byte(tc.body), tc.cb, tc.sig); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
