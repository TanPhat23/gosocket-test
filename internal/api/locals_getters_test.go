package api

import (
	"net/http/httptest"
	"testing"

	"agent-task-manager/internal/models"

	"github.com/gofiber/fiber/v3"
)

func TestLocalsGetters(t *testing.T) {
	user := &models.User{ID: "u1"}
	tok := &models.APIToken{}
	var typedNilUser *models.User

	cases := []struct {
		name     string
		set      func(c fiber.Ctx)
		wantUser *models.User
		wantTok  *models.APIToken
		wantRole models.TeamRole
	}{
		{"missing", func(fiber.Ctx) {}, nil, nil, 0},
		{"nil values", func(c fiber.Ctx) {
			c.Locals(LocalsUserKey, nil)
			c.Locals(LocalsAPITokenKey, nil)
			c.Locals(LocalsTeamRoleKey, nil)
		}, nil, nil, 0},
		{"wrong types", func(c fiber.Ctx) {
			c.Locals(LocalsUserKey, "str")
			c.Locals(LocalsAPITokenKey, 42)
			c.Locals(LocalsTeamRoleKey, "admin")
		}, nil, nil, 0},
		{"value instead of pointer", func(c fiber.Ctx) {
			c.Locals(LocalsUserKey, models.User{ID: "x"})
		}, nil, nil, 0},
		{"typed nil pointer", func(c fiber.Ctx) {
			c.Locals(LocalsUserKey, typedNilUser)
		}, nil, nil, 0},
		{"correct types", func(c fiber.Ctx) {
			c.Locals(LocalsUserKey, user)
			c.Locals(LocalsAPITokenKey, tok)
			c.Locals(LocalsTeamRoleKey, models.TeamRole(3))
		}, user, tok, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c fiber.Ctx) error {
				tc.set(c)
				if got := CurrentUser(c); got != tc.wantUser {
					t.Errorf("CurrentUser = %v, want %v", got, tc.wantUser)
				}
				if got := CurrentAPIToken(c); got != tc.wantTok {
					t.Errorf("CurrentAPIToken = %v, want %v", got, tc.wantTok)
				}
				if got := CurrentTeamRole(c); got != tc.wantRole {
					t.Errorf("CurrentTeamRole = %v, want %v", got, tc.wantRole)
				}
				return c.SendStatus(fiber.StatusOK)
			})
			resp, err := app.Test(httptest.NewRequest("GET", "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
		})
	}
}
