package api

import (
	"net/http/httptest"
	"testing"

	"agent-task-manager/internal/models"

	"github.com/gofiber/fiber/v3"
)

func TestCurrentUserID(t *testing.T) {
	app := fiber.New()
	app.Get("/anon", func(c fiber.Ctx) error { return c.SendString(currentUserID(c)) })
	app.Get("/user", func(c fiber.Ctx) error {
		c.Locals(LocalsUserKey, &models.User{ID: "u1"})
		return c.SendString(currentUserID(c))
	})
	for path, want := range map[string]string{"/anon": "", "/user": "u1"} {
		resp, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 16)
		n, _ := resp.Body.Read(buf)
		if got := string(buf[:n]); got != want {
			t.Errorf("%s: got %q want %q", path, got, want)
		}
	}
}
