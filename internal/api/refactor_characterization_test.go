package api

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/models"
	"agent-task-manager/internal/service"

	"github.com/gofiber/fiber/v3"
)

// Stubs embed the service interface so only the methods under test are implemented.
type charCredService struct {
	service.CredentialService
	calls int
	last  any
}

func (s *charCredService) Create(_ context.Context, _ string, req dto.CreateCredentialRequest) (*dto.CredentialResponseDTO, error) {
	s.calls++
	s.last = req
	return &dto.CredentialResponseDTO{ID: "c1", Label: req.Label}, nil
}

func (s *charCredService) Update(_ context.Context, _, _ string, req dto.UpdateCredentialRequest) (*dto.CredentialResponseDTO, error) {
	s.calls++
	s.last = req
	return &dto.CredentialResponseDTO{ID: "c1"}, nil
}

type charIntegService struct {
	service.IntegrationService
	calls int
}

func (s *charIntegService) CreateIntegration(_ context.Context, _ string, _ dto.CreateIntegrationRequest) (*dto.IntegrationResponse, error) {
	s.calls++
	return &dto.IntegrationResponse{ID: "i1"}, nil
}

func (s *charIntegService) UpdateIntegration(_ context.Context, _, _ string, _ dto.UpdateIntegrationRequest) (*dto.IntegrationResponse, error) {
	s.calls++
	return &dto.IntegrationResponse{ID: "i1"}, nil
}

type charTaskService struct {
	service.TaskService
	calls int
}

func (s *charTaskService) CreateTask(_ context.Context, req *dto.CreateTaskRequest) (*models.Task, error) {
	s.calls++
	return &models.Task{ID: "t1", Title: req.Title}, nil
}

func (s *charTaskService) PromptTaskSession(_ context.Context, _, _ string, _ *dto.TaskSessionPromptRequest) error {
	s.calls++
	return nil
}

type charResult struct {
	status int
	body   string
}

func charDo(t *testing.T, app *fiber.App, method, path, ctype, body string) charResult {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return charResult{resp.StatusCode, string(b)}
}

func TestBindBodyHandlersCharacterization(t *testing.T) {
	cred := &charCredService{}
	integ := &charIntegService{}
	task := &charTaskService{}
	ch := NewCredentialHandlers(cred)
	ih := NewIntegrationHandlers(integ)
	th := NewTaskHandlers(task)

	app := fiber.New(fiber.Config{StructValidator: NewStructValidator(), ErrorHandler: CentralErrorHandler})
	app.Post("/teams/:team_id/credentials", ch.Create)
	app.Put("/teams/:team_id/credentials/:credential_id", ch.Update)
	app.Post("/projects/:project_id/integrations", ih.Create)
	app.Put("/projects/:project_id/integrations/:id", ih.Update)
	app.Post("/tasks", th.Create)
	app.Post("/tasks/:id/session/prompt", th.PromptSession)

	const js = "application/json"
	const uuid = "123e4567-e89b-12d3-a456-426614174000"
	type endpoint struct {
		name, method, path string
		valid, invalid     string // valid body; body failing validation
		okStatus           int
		calls              func() int
	}
	eps := []endpoint{
		{"cred create", "POST", "/teams/t/credentials", `{"provider":"github","label":"l","secret":"s"}`, `{"provider":"bogus","label":"l","secret":"s"}`, 201, func() int { return cred.calls }},
		{"cred update", "PUT", "/teams/t/credentials/c", `{"label":"l"}`, `{"label":""}`, 200, func() int { return cred.calls }},
		{"integ create", "POST", "/projects/p/integrations", `{"credential_id":"` + uuid + `","provider":"github","repo":"o/r"}`, `{"provider":"github"}`, 201, func() int { return integ.calls }},
		{"integ update", "PUT", "/projects/p/integrations/i", `{"enabled":true}`, `{"credential_id":"nope"}`, 200, func() int { return integ.calls }},
		{"task create", "POST", "/tasks", `{"project_id":"` + uuid + `","title":"t","prompt":"p","assignee_id":"a","assignee_type":"agent","priority":50}`, `{"title":"t"}`, 201, func() int { return task.calls }},
		{"task prompt", "POST", "/tasks/x/session/prompt", `{"prompt":"hi"}`, `{"prompt":""}`, 200, func() int { return task.calls }},
	}
	cases := []struct {
		name, ctype string
		body        func(e endpoint) string
	}{
		{"malformed", js, func(endpoint) string { return `{"x":` }},
		{"empty body json", js, func(endpoint) string { return `` }},
		{"empty body no ctype", "", func(endpoint) string { return `` }},
		{"wrong content type", "text/plain", func(e endpoint) string { return e.valid }},
		{"unknown field", js, func(e endpoint) string { return strings.Replace(e.valid, "{", `{"zzz_unknown":1,`, 1) }},
		{"validation failure", js, func(e endpoint) string { return e.invalid }},
		{"success", js, func(e endpoint) string { return e.valid }},
	}
	for _, e := range eps {
		for _, c := range cases {
			before := e.calls()
			r := charDo(t, app, e.method, e.path, c.ctype, c.body(e))
			key := e.name + "|" + c.name
			if want, ok := charGolden[key]; ok {
				got := charGoldenEntry{r.status, r.body, e.calls() - before}
				if got != want {
					t.Errorf("%s: got %+v want %+v", key, got, want)
				}
			} else {
				t.Errorf("%s: no golden", key)
			}
		}
	}
}

type charGoldenEntry struct {
	status int
	body   string
	calls  int
}

var charGolden = map[string]charGoldenEntry{
	"cred create|malformed":            {400, "{\"error\":\"bind from body: unexpected end of JSON input\"}", 0},
	"cred create|empty body json":      {400, "{\"error\":\"bind from body: unexpected end of JSON input\"}", 0},
	"cred create|empty body no ctype":  {400, "{\"error\":\"Unprocessable Entity\"}", 0},
	"cred create|wrong content type":   {400, "{\"error\":\"Unprocessable Entity\"}", 0},
	"cred create|unknown field":        {201, "{\"credential\":{\"id\":\"c1\",\"team_id\":\"\",\"provider\":\"\",\"label\":\"l\",\"created_at\":\"0001-01-01T00:00:00Z\",\"updated_at\":\"0001-01-01T00:00:00Z\"}}", 1},
	"cred create|validation failure":   {400, "{\"error\":\"Key: 'CreateCredentialRequest.Provider' Error:Field validation for 'Provider' failed on the 'oneof' tag\"}", 0},
	"cred create|success":              {201, "{\"credential\":{\"id\":\"c1\",\"team_id\":\"\",\"provider\":\"\",\"label\":\"l\",\"created_at\":\"0001-01-01T00:00:00Z\",\"updated_at\":\"0001-01-01T00:00:00Z\"}}", 1},
	"cred update|malformed":            {400, "{\"error\":\"bind from body: unexpected end of JSON input\"}", 0},
	"cred update|empty body json":      {400, "{\"error\":\"bind from body: unexpected end of JSON input\"}", 0},
	"cred update|empty body no ctype":  {400, "{\"error\":\"Unprocessable Entity\"}", 0},
	"cred update|wrong content type":   {400, "{\"error\":\"Unprocessable Entity\"}", 0},
	"cred update|unknown field":        {200, "{\"credential\":{\"id\":\"c1\",\"team_id\":\"\",\"provider\":\"\",\"label\":\"\",\"created_at\":\"0001-01-01T00:00:00Z\",\"updated_at\":\"0001-01-01T00:00:00Z\"}}", 1},
	"cred update|validation failure":   {400, "{\"error\":\"Key: 'UpdateCredentialRequest.Label' Error:Field validation for 'Label' failed on the 'min' tag\"}", 0},
	"cred update|success":              {200, "{\"credential\":{\"id\":\"c1\",\"team_id\":\"\",\"provider\":\"\",\"label\":\"\",\"created_at\":\"0001-01-01T00:00:00Z\",\"updated_at\":\"0001-01-01T00:00:00Z\"}}", 1},
	"integ create|malformed":           {400, "{\"error\":\"bind from body: unexpected end of JSON input\"}", 0},
	"integ create|empty body json":     {400, "{\"error\":\"bind from body: unexpected end of JSON input\"}", 0},
	"integ create|empty body no ctype": {400, "{\"error\":\"Unprocessable Entity\"}", 0},
	"integ create|wrong content type":  {400, "{\"error\":\"Unprocessable Entity\"}", 0},
	"integ create|unknown field":       {201, "{\"id\":\"i1\",\"project_id\":\"\",\"credential_id\":\"\",\"provider\":\"\",\"external_id\":\"\",\"external_url\":\"\",\"enabled\":false,\"config\":null,\"created_at\":\"0001-01-01T00:00:00Z\",\"updated_at\":\"0001-01-01T00:00:00Z\"}", 1},
	"integ create|validation failure":  {400, "{\"error\":\"Key: 'CreateIntegrationRequest.CredentialID' Error:Field validation for 'CredentialID' failed on the 'required' tag\\nKey: 'CreateIntegrationRequest.Repo' Error:Field validation for 'Repo' failed on the 'required' tag\"}", 0},
	"integ create|success":             {201, "{\"id\":\"i1\",\"project_id\":\"\",\"credential_id\":\"\",\"provider\":\"\",\"external_id\":\"\",\"external_url\":\"\",\"enabled\":false,\"config\":null,\"created_at\":\"0001-01-01T00:00:00Z\",\"updated_at\":\"0001-01-01T00:00:00Z\"}", 1},
	"integ update|malformed":           {400, "{\"error\":\"bind from body: unexpected end of JSON input\"}", 0},
	"integ update|empty body json":     {400, "{\"error\":\"bind from body: unexpected end of JSON input\"}", 0},
	"integ update|empty body no ctype": {400, "{\"error\":\"Unprocessable Entity\"}", 0},
	"integ update|wrong content type":  {400, "{\"error\":\"Unprocessable Entity\"}", 0},
	"integ update|unknown field":       {200, "{\"id\":\"i1\",\"project_id\":\"\",\"credential_id\":\"\",\"provider\":\"\",\"external_id\":\"\",\"external_url\":\"\",\"enabled\":false,\"config\":null,\"created_at\":\"0001-01-01T00:00:00Z\",\"updated_at\":\"0001-01-01T00:00:00Z\"}", 1},
	"integ update|validation failure":  {400, "{\"error\":\"Key: 'UpdateIntegrationRequest.CredentialID' Error:Field validation for 'CredentialID' failed on the 'uuid' tag\"}", 0},
	"integ update|success":             {200, "{\"id\":\"i1\",\"project_id\":\"\",\"credential_id\":\"\",\"provider\":\"\",\"external_id\":\"\",\"external_url\":\"\",\"enabled\":false,\"config\":null,\"created_at\":\"0001-01-01T00:00:00Z\",\"updated_at\":\"0001-01-01T00:00:00Z\"}", 1},
	"task create|malformed":            {400, "{\"error\":\"bind from body: unexpected end of JSON input\"}", 0},
	"task create|empty body json":      {400, "{\"error\":\"bind from body: unexpected end of JSON input\"}", 0},
	"task create|empty body no ctype":  {400, "{\"error\":\"Unprocessable Entity\"}", 0},
	"task create|wrong content type":   {400, "{\"error\":\"Unprocessable Entity\"}", 0},
	"task create|unknown field":        {201, "{\"id\":\"t1\",\"project_id\":\"\",\"title\":\"t\",\"prompt\":\"\",\"assignee_id\":\"\",\"assignee_type\":\"\",\"status\":\"\",\"priority\":0,\"skills\":[],\"mcp_servers\":[],\"created_at\":\"0001-01-01T00:00:00Z\",\"updated_at\":\"0001-01-01T00:00:00Z\"}", 1},
	"task create|validation failure":   {400, "{\"error\":\"Key: 'CreateTaskRequest.ProjectID' Error:Field validation for 'ProjectID' failed on the 'required' tag\\nKey: 'CreateTaskRequest.Prompt' Error:Field validation for 'Prompt' failed on the 'required' tag\\nKey: 'CreateTaskRequest.AssigneeID' Error:Field validation for 'AssigneeID' failed on the 'required' tag\\nKey: 'CreateTaskRequest.AssigneeType' Error:Field validation for 'AssigneeType' failed on the 'required' tag\\nKey: 'CreateTaskRequest.Priority' Error:Field validation for 'Priority' failed on the 'task_priority' tag\"}", 0},
	"task create|success":              {201, "{\"id\":\"t1\",\"project_id\":\"\",\"title\":\"t\",\"prompt\":\"\",\"assignee_id\":\"\",\"assignee_type\":\"\",\"status\":\"\",\"priority\":0,\"skills\":[],\"mcp_servers\":[],\"created_at\":\"0001-01-01T00:00:00Z\",\"updated_at\":\"0001-01-01T00:00:00Z\"}", 1},
	"task prompt|malformed":            {400, "{\"error\":\"bind from body: unexpected end of JSON input\"}", 0},
	"task prompt|empty body json":      {400, "{\"error\":\"bind from body: unexpected end of JSON input\"}", 0},
	"task prompt|empty body no ctype":  {400, "{\"error\":\"Unprocessable Entity\"}", 0},
	"task prompt|wrong content type":   {400, "{\"error\":\"Unprocessable Entity\"}", 0},
	"task prompt|unknown field":        {200, "{\"status\":\"ok\"}", 1},
	"task prompt|validation failure":   {400, "{\"error\":\"Key: 'TaskSessionPromptRequest.Prompt' Error:Field validation for 'Prompt' failed on the 'required' tag\"}", 0},
	"task prompt|success":              {200, "{\"status\":\"ok\"}", 1},
}
