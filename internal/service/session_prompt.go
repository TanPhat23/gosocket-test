package service

import (
	"context"
	"encoding/base64"

	"agent-task-manager/internal/common"
	"agent-task-manager/internal/dto"
	"agent-task-manager/internal/runner"
)

// Limits on the images a steer can carry.
const (
	maxSteerImages     = 5
	maxSteerImageBytes = 5 << 20
)

var steerImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

func badSteer(message string) error {
	return &common.AppError{Code: 400, Message: message}
}

// steerFiles checks the images of a steer and turns them into the runner's files.
func steerFiles(images []dto.SessionImageDTO) ([]runner.PromptFile, error) {
	if len(images) > maxSteerImages {
		return nil, badSteer("at most 5 images can be attached to a steer")
	}
	files := make([]runner.PromptFile, 0, len(images))
	for _, img := range images {
		if !steerImageTypes[img.MIME] {
			return nil, badSteer("images must be PNG, JPEG, GIF or WebP")
		}
		raw, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil || len(raw) == 0 {
			return nil, badSteer("an attached image is not valid base64 data")
		}
		if len(raw) > maxSteerImageBytes {
			return nil, badSteer("each image must be 5 MB or smaller")
		}
		files = append(files, runner.PromptFile{MIME: img.MIME, Name: img.Name, Data: img.Data})
	}
	return files, nil
}

// sendSteer delivers a steer to the session. Without images it is the plain text prompt every client accepts.
func sendSteer(ctx context.Context, oc runner.OpenCodeClient, sessionID, prompt string, files []runner.PromptFile) error {
	if len(files) == 0 {
		return oc.PromptSession(ctx, sessionID, prompt)
	}
	sender, ok := oc.(runner.FilePromptSender)
	if !ok {
		return badSteer("this session cannot take images")
	}
	return sender.PromptSessionWithFiles(ctx, sessionID, prompt, files)
}
