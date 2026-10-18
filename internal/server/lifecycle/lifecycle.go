// Package lifecycle runs the control plane's background work and its HTTP server until a shutdown signal.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"

	"agent-task-manager/internal/gateway"
)

// StartGateway runs the credential gateway in the background. The returned function stops it.
func StartGateway(svc *gateway.Service) (func(), error) {
	ln, err := svc.Listen()
	if err != nil {
		return nil, fmt.Errorf("gateway: listen: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := svc.Run(ctx, ln); err != nil {
			log.Printf("gateway: stopped: %v", err)
		}
	}()
	log.Printf("LLM gateway listening on %s", ln.Addr())
	return cancel, nil
}

// Reconciler is the periodic upkeep of tasks: merged pull requests and finished workspaces.
type Reconciler interface {
	ReconcileActivePRs(ctx context.Context) error
	CleanupFinishedWorkspaces(ctx context.Context) error
}

// StartReconciler runs the upkeep every thirty seconds until ctx is cancelled.
func StartReconciler(ctx context.Context, r Reconciler) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = r.ReconcileActivePRs(ctx)
				_ = r.CleanupFinishedWorkspaces(ctx)
			}
		}
	}()
}

// ServeUntilSignal listens on port until an interrupt or SIGTERM, then shuts the HTTP server down within ten seconds.
// A listen failure other than a closed server is returned.
func ServeUntilSignal(app *fiber.App, port int) error {
	serverErrCh := make(chan error, 1)
	go func() {
		addr := fmt.Sprintf(":%d", port)
		log.Printf("Listening and serving on %s", addr)
		if err := app.Listen(addr); err != nil {
			serverErrCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("Received shutdown signal: %v. Initiating graceful shutdown...", sig)
	case err := <-serverErrCh:
		if !errors.Is(err, os.ErrClosed) {
			return fmt.Errorf("server listen error: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Printf("Error shutting down server: %v", err)
	}
	log.Println("Server gracefully stopped")
	return nil
}
