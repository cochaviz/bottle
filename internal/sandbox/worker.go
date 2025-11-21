package sandbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// SandboxWorkerSignalType enumerates the supported worker control signals.
type SandboxWorkerSignalType string

const (
	// SandboxWorkerSignalExecuteCommand instructs the worker to run a command via the driver.
	SandboxWorkerSignalExecuteCommand SandboxWorkerSignalType = "execute_command"
	// SandboxWorkerSignalStop requests the worker to stop and release the underlying sandbox.
	SandboxWorkerSignalStop SandboxWorkerSignalType = "stop"
)

// SandboxWorkerSignalResponse wraps the outcome of a worker signal.
type SandboxWorkerSignalResponse struct {
	Result *SandboxCommandResult
	Err    error
}

// SandboxWorkerSignal represents an instruction destined to a SandboxWorker instance.
type SandboxWorkerSignal struct {
	Type     SandboxWorkerSignalType
	Command  *SandboxCommand
	Response chan SandboxWorkerSignalResponse
}

func (s SandboxWorkerSignal) respond(resp SandboxWorkerSignalResponse) {
	if s.Response == nil {
		return
	}

	s.Response <- resp
}

var errSandboxWorkerStop = errors.New("sandbox worker stop requested")

// SandboxWorker handles the execution, analysis, instrumentation
// of a sandboxed sample. Only a single SandboxWorker can access a
// particular sandbox.
type SandboxWorker struct {
	lease  SandboxLease
	driver SandboxDriver

	logger *slog.Logger

	signals       chan SandboxWorkerSignal
	startNotifier chan struct{}
}

// NewSandboxWorker creates a new SandboxWorker instance with the provided driver and lease.
func NewSandboxWorker(driver SandboxDriver, lease SandboxLease, logger *slog.Logger) *SandboxWorker {
	return &SandboxWorker{
		driver:  driver,
		lease:   lease,
		logger:  logger,
		signals: make(chan SandboxWorkerSignal, 8),
	}
}

func (w *SandboxWorker) SetStartNotifier(ch chan struct{}) {
	w.startNotifier = ch
}

// SignalChannel returns a send-only channel that callers can use to control the worker.
func (w *SandboxWorker) SignalChannel() chan<- SandboxWorkerSignal {
	return w.signals
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
	if w.startNotifier != nil {
		close(w.startNotifier)
		w.startNotifier = nil
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case signal := <-w.signals:
			if err := w.handleSignal(ctx, signal); err != nil {
				if errors.Is(err, errSandboxWorkerStop) {
					return nil
				}
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			}
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

func (w *SandboxWorker) handleSignal(ctx context.Context, signal SandboxWorkerSignal) error {
	switch signal.Type {
	case SandboxWorkerSignalExecuteCommand:
		if signal.Command == nil {
			err := fmt.Errorf("sandbox worker execute signal requires a command")
			w.logger.Error(err.Error())
			signal.respond(SandboxWorkerSignalResponse{Err: err})
			return nil
		}
		resultCh := make(chan SandboxWorkerSignalResponse, 1)
		go func() {
			result, err := w.driver.Execute(w.lease, *signal.Command)
			if err != nil {
				w.logger.Error("sandbox command failed", "error", err, "path", signal.Command.Path)
			} else {
				w.logger.Info("sandbox command completed", "path", signal.Command.Path)
			}
			resCopy := result
			resultCh <- SandboxWorkerSignalResponse{
				Result: &resCopy,
				Err:    err,
			}
		}()

		select {
		case resp := <-resultCh:
			signal.respond(resp)
		case <-ctx.Done():
			signal.respond(SandboxWorkerSignalResponse{Err: ctx.Err()})
			return ctx.Err()
		}
		return nil
	case SandboxWorkerSignalStop:
		w.logger.Info("sandbox stop signal received")
		signal.respond(SandboxWorkerSignalResponse{})
		return errSandboxWorkerStop
	default:
		err := fmt.Errorf("unsupported sandbox worker signal: %s", signal.Type)
		w.logger.Error(err.Error())
		signal.respond(SandboxWorkerSignalResponse{Err: err})
		return nil
	}
}
