package service_test

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/repository"
	"agent-task-manager/internal/runner"
	"agent-task-manager/internal/service"
)

// filesClient is an OpenCode client that also takes files with a prompt.
type filesClient struct {
	tunnelClient
	files []runner.PromptFile
	text  string
}

func (c *filesClient) PromptSessionWithFiles(_ context.Context, _ string, prompt string, files []runner.PromptFile) error {
	c.text = prompt
	c.files = files
	return nil
}

func imageDTO(data []byte) dto.SessionImageDTO {
	return dto.SessionImageDTO{Name: "shot.png", MIME: "image/png", Data: base64.StdEncoding.EncodeToString(data)}
}

func isBadRequest(err error) bool {
	var app *common.AppError
	return errors.As(err, &app) && app.Code == 400
}

func TestSteerFiles_RejectsBadImages(t *testing.T) {
	cases := map[string][]dto.SessionImageDTO{
		"too many":   {imageDTO([]byte("a")), imageDTO([]byte("b")), imageDTO([]byte("c")), imageDTO([]byte("d")), imageDTO([]byte("e")), imageDTO([]byte("f"))},
		"bad type":   {{Name: "x.svg", MIME: "image/svg+xml", Data: base64.StdEncoding.EncodeToString([]byte("<svg/>"))}},
		"bad base64": {{Name: "x.png", MIME: "image/png", Data: "not base64!"}},
		"too big":    {imageDTO([]byte(strings.Repeat("x", 5<<20+1)))},
	}
	for name, images := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			fx := newTunnelFixture(t, true)
			err := fx.svc.PromptTaskSession(ctx, fx.taskA.ID, fx.memberA.ID, &dto.TaskSessionPromptRequest{Prompt: "look", Images: images})
			if !isBadRequest(err) {
				t.Fatalf("err = %v, want a 400", err)
			}
		})
	}
}

func TestPromptTaskSession_SendsImagesToAFileCapableClient(t *testing.T) {
	ctx := context.Background()
	fx := newTunnelFixture(t, true)
	client := &filesClient{}
	resolve := func(_ context.Context, projectID string) (runner.OpenCodeClient, error) {
		if projectID == fx.projA.ID {
			return client, nil
		}
		return nil, errors.New("unexpected project")
	}
	svc := service.NewTaskService(fx.spy, repository.NewAttemptRepository(fx.db), fx.queue, nil,
		service.WithTaskSessionDependencies(nil, repository.NewProjectRepository(fx.db), repository.NewTeamRepository(fx.db)),
		service.WithTaskSessionClientResolver(resolve))

	req := &dto.TaskSessionPromptRequest{Prompt: "compare these", Images: []dto.SessionImageDTO{imageDTO([]byte("png"))}}
	if err := svc.PromptTaskSession(ctx, fx.taskA.ID, fx.memberA.ID, req); err != nil {
		t.Fatalf("PromptTaskSession: %v", err)
	}

	if client.text != "compare these" || len(client.files) != 1 || client.files[0].MIME != "image/png" {
		t.Fatalf("client got text %q and files %+v", client.text, client.files)
	}
}

func TestPromptTaskSession_RefusesImagesWhenTheClientCannotTakeThem(t *testing.T) {
	ctx := context.Background()
	fx := newTunnelFixture(t, true)

	req := &dto.TaskSessionPromptRequest{Prompt: "look", Images: []dto.SessionImageDTO{imageDTO([]byte("png"))}}
	err := fx.svc.PromptTaskSession(ctx, fx.taskA.ID, fx.memberA.ID, req)

	if !isBadRequest(err) {
		t.Fatalf("err = %v, want a 400 for a client without file support", err)
	}
}
