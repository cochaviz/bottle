package sandbox

import (
	"context"
	"errors"
	"fmt"
)

// SandboxWorker handles the execution, analysis, instrumentation
// of a sandboxed sample. Only a single SandboxWorker can access a
// particular sandbox.
type SandboxWorker struct {
	lease  SandboxLease
	driver SandboxDriver
}

// NewSandboxWorker creates a new SandboxWorker instance with the provided driver and lease.
func NewSandboxWorker(driver SandboxDriver, lease SandboxLease) *SandboxWorker {
	return &SandboxWorker{
		driver: driver,
		lease:  lease,
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
		if destroyErr := w.driver.Destroy(w.lease, true); destroyErr != nil {
			err = errors.Join(err, fmt.Errorf("destroy sandbox: %w", destroyErr))
		}
	}()

	updatedLease, err := w.driver.Start(w.lease)
	if err != nil {
		return err
	}
	w.lease = updatedLease

	// Wait until the context is cancelled.
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
		return ctx.Err()
	}
}
