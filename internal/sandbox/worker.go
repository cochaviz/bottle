package sandbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// SandboxWorker handles the execution, analysis, instrumentation
// of a sandboxed sample. Only a single SandboxWorker can access a
// particular sandbox.
type SandboxWorker struct {
	lease  SandboxLease
	driver SandboxDriver

	logger *slog.Logger
}

// NewSandboxWorker creates a new SandboxWorker instance with the provided driver and lease.
func NewSandboxWorker(driver SandboxDriver, lease SandboxLease, logger *slog.Logger) *SandboxWorker {
	return &SandboxWorker{
		driver: driver,
		lease:  lease,
		logger: logger,
	}
}

// Run executes the sandboxed sample.
func (w *SandboxWorker) Run(ctx context.Context) (err error) {
	if w.driver == nil {
		return fmt.Errorf("driver not initialized")
	}
	if w.lease.ID == "" {
		return fmt.Errorf("lease not initialized")
	}

	defer func() {
		if releaseError := w.driver.Release(w.lease, true); releaseError != nil {
			err = errors.Join(err, fmt.Errorf("destroy sandbox: %w", releaseError))
		}
	}()

	updatedLease, err := w.driver.Start(w.lease)
	if err != nil {
		return err
	}
	w.lease = updatedLease

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return ctx.Err()
		case <-ticker.C:
			// Check whether the sandbox is still active
			if _, err := w.driver.CollectMetrics(w.lease); err != nil {
				w.logger.Error("Failed to collect metrics", "error", err)
				return err
			}
		}
	}
}
