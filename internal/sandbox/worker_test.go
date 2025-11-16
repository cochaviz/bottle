package sandbox

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestSandboxWorkerHandlesSignals(t *testing.T) {
	driver := &stubSandboxDriver{
		executeResult: SandboxCommandResult{
			Stdout:   "done",
			Stderr:   "",
			ExitCode: 0,
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	lease := SandboxLease{
		ID: "lease-test",
	}

	worker := NewSandboxWorker(driver, lease, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- worker.Run(ctx)
	}()

	execResp := make(chan SandboxWorkerSignalResponse, 1)
	worker.SignalChannel() <- SandboxWorkerSignal{
		Type: SandboxWorkerSignalExecuteCommand,
		Command: &SandboxCommand{
			Path: "/bin/true",
		},
		Response: execResp,
	}

	select {
	case resp := <-execResp:
		if resp.Err != nil {
			t.Fatalf("execute signal returned error: %v", resp.Err)
		}
		if resp.Result == nil || resp.Result.Stdout != "done" {
			t.Fatalf("unexpected command result: %#v", resp.Result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for execute response")
	}

	stopResp := make(chan SandboxWorkerSignalResponse, 1)
	worker.SignalChannel() <- SandboxWorkerSignal{
		Type:     SandboxWorkerSignalStop,
		Response: stopResp,
	}

	select {
	case <-stopResp:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stop response")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("worker returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not exit after stop signal")
	}

	if !driver.releaseCalled {
		t.Fatal("driver release not called")
	}
	if len(driver.executedCommands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(driver.executedCommands))
	}
}

type stubSandboxDriver struct {
	executeResult SandboxCommandResult
	executeErr    error

	releaseCalled    bool
	executedCommands []SandboxCommand
}

func (d *stubSandboxDriver) Acquire(spec SandboxLeaseSpecification) (SandboxLease, error) {
	return SandboxLease{}, nil
}

func (d *stubSandboxDriver) Start(lease SandboxLease) (SandboxLease, error) {
	return lease, nil
}

func (d *stubSandboxDriver) Execute(lease SandboxLease, command SandboxCommand) (SandboxCommandResult, error) {
	d.executedCommands = append(d.executedCommands, command)
	if d.executeErr != nil {
		return d.executeResult, d.executeErr
	}
	return d.executeResult, nil
}

func (d *stubSandboxDriver) Pause(lease SandboxLease, reason string) (SandboxLease, error) {
	return lease, nil
}

func (d *stubSandboxDriver) Resume(lease SandboxLease) (SandboxLease, error) {
	return lease, nil
}

func (d *stubSandboxDriver) Release(lease SandboxLease, force bool) error {
	d.releaseCalled = true
	return nil
}

func (d *stubSandboxDriver) CollectMetrics(lease SandboxLease) (SandboxMetrics, error) {
	return SandboxMetrics{}, nil
}
