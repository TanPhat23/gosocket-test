package api

import (
	"os"
	"strings"
	"unicode"

	"github.com/gofiber/fiber/v3"
)

const (
	minProviderKeyLen = 8
	maxProviderKeyLen = 512
)

// OperatorHandlers manage site-wide runtime settings. Only accounts named in ATM_OPERATOR_EMAILS may use them,
// and they never see a secret back: the provider key is write-only.
type OperatorHandlers struct {
	keyFile   string
	operators map[string]bool
}

// NewOperatorHandlers builds the handlers. keyFile is the gateway's ATM_GATEWAY_PROVIDER_KEY_FILE ("" when the
// gateway takes its key another way, in which case writes are refused).
func NewOperatorHandlers(keyFile string, operatorEmails []string) *OperatorHandlers {
	ops := make(map[string]bool, len(operatorEmails))
	for _, e := range operatorEmails {
		ops[strings.ToLower(strings.TrimSpace(e))] = true
	}
	return &OperatorHandlers{keyFile: keyFile, operators: ops}
}

// isOperator requires an interactive session: scoped personal access tokens never act as operators.
func (h *OperatorHandlers) isOperator(c fiber.Ctx) bool {
	u := CurrentUser(c)
	return u != nil && CurrentAPIToken(c) == nil && h.operators[strings.ToLower(u.Email)]
}

// GetProviderKeyStatus handles GET /api/v1/operator/provider-key. Anyone signed in may ask whether they are an
// operator; only operators learn whether a key is configured. The key itself is never returned.
func (h *OperatorHandlers) GetProviderKeyStatus(c fiber.Ctx) error {
	if !h.isOperator(c) {
		return c.JSON(fiber.Map{"operator": false})
	}
	out := fiber.Map{"operator": true, "writable": h.keyFile != "", "configured": false}
	if h.keyFile != "" {
		if st, err := os.Stat(h.keyFile); err == nil && st.Size() > 0 {
			out["configured"] = true
			out["updated_at"] = st.ModTime().UTC()
		}
	}
	return c.JSON(out)
}

// SetProviderKey handles PUT /api/v1/operator/provider-key. The gateway re-reads the file on every request, so the
// new key is live immediately. The file is written in place (it is a bind mount, so it cannot be replaced by rename).
func (h *OperatorHandlers) SetProviderKey(c fiber.Ctx) error {
	if !h.isOperator(c) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "operator access required"})
	}
	if h.keyFile == "" {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "the gateway provider key is not file-based on this server"})
	}
	var req struct {
		Key string `json:"key"`
	}
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	key := strings.TrimSpace(req.Key)
	if len(key) < minProviderKeyLen || len(key) > maxProviderKeyLen {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "key length is out of range"})
	}
	for _, r := range key {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "key must not contain whitespace or control characters"})
		}
	}
	f, err := os.OpenFile(h.keyFile, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "the provider key file is not writable"})
	}
	_, werr := f.WriteString(key)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to write the provider key"})
	}
	return c.JSON(fiber.Map{"status": "ok"})
}
