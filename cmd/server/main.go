package main

import (
	"fmt"
	"log"
	"os"

	"agent-task-manager/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	fmt.Printf("Starting Agent Task Manager Control Plane on port %d (env: %s)\n", cfg.Port, cfg.AppEnv)
	// Server initialization and lifecycle hooks will be integrated in subsequent tasks
	_ = os.Stdout
}
