package analysis

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cochaviz/bottle/internal/sandbox"
)

func TestStartInstrumentationSkipsMissingC2(t *testing.T) {
	t.Parallel()

	inst := &fakeInstrumentation{
		name:     "tcpdump",
		requires: []InstrumentationVariableName{InstrumentationC2Address},
	}
	worker := &AnalysisWorker{
		logger:          newTestLogger(),
		c2Ip:            "",
		sample:          Sample{Name: "sample", Artifact: filepath.Join(t.TempDir(), "sample.bin")},
		instrumentation: []Instrumentation{inst},
	}
	worker.logDir = t.TempDir()

	lease := newTestLease(t)

	started, cleanup, err := worker.startInstrumentation(context.Background(), &lease)
	if err != nil {
		t.Fatalf("startInstrumentation() error = %v", err)
	}
	if len(started) != 0 {
		t.Fatalf("startInstrumentation() started %d instrumentations, want 0", len(started))
	}
	if cleanup != nil {
		t.Fatal("cleanup should be nil when no instrumentation started")
	}
	if inst.started {
		t.Fatal("instrumentation should not start when required C2 address missing")
	}
}

func TestStartInstrumentationRunsWhenRequirementsMet(t *testing.T) {
	t.Parallel()

	inst := &fakeInstrumentation{
		name:     "tcpdump",
		requires: []InstrumentationVariableName{InstrumentationC2Address},
	}
	worker := &AnalysisWorker{
		logger:          newTestLogger(),
		c2Ip:            "203.0.113.10",
		sample:          Sample{Name: "sample", Artifact: filepath.Join(t.TempDir(), "sample.bin")},
		instrumentation: []Instrumentation{inst},
	}
	worker.logDir = t.TempDir()

	lease := newTestLease(t)

	started, cleanup, err := worker.startInstrumentation(context.Background(), &lease)
	if err != nil {
		t.Fatalf("startInstrumentation() error = %v", err)
	}
	if len(started) != 1 {
		t.Fatalf("startInstrumentation() started %d instrumentations, want 1", len(started))
	}
	if cleanup == nil {
		t.Fatal("cleanup should not be nil when instrumentation started")
	}
	if !inst.started {
		t.Fatal("instrumentation should have been started when requirements are met")
	}
}

func TestEnsureInstrumentationRunningFailure(t *testing.T) {
	t.Parallel()

	inst := &fakeInstrumentation{
		name:     "tcpdump",
		started:  true,
		running:  false,
		runError: errors.New("exited"),
	}
	worker := &AnalysisWorker{
		logger: newTestLogger(),
	}
	err := worker.ensureInstrumentationRunning([]Instrumentation{inst})
	if err == nil {
		t.Fatal("ensureInstrumentationRunning() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "tcpdump") {
		t.Fatalf("ensureInstrumentationRunning() error = %q, want instrumentation name", err)
	}
}

func newTestLease(t *testing.T) sandbox.SandboxLease {
	t.Helper()
	return sandbox.SandboxLease{
		Metadata: map[string]any{
			"vm_interface": "eth0",
			"vm_ip":        "10.0.0.10",
		},
		RunDir: t.TempDir(),
	}
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

type fakeInstrumentation struct {
	name     string
	requires []InstrumentationVariableName

	startErr error
	runError error
	closeErr error

	started bool
	running bool
}

func (f *fakeInstrumentation) Start(ctx context.Context, lease sandbox.SandboxLease, variables ...InstrumentationVariable) error {
	if err := ensureRequiredInstrumentationVariables(variables, f.requires, f.Name()); err != nil {
		return err
	}
	if f.startErr != nil {
		return f.startErr
	}
	f.started = true
	f.running = true
	return nil
}

func (f *fakeInstrumentation) Close() error {
	f.running = false
	if f.closeErr != nil {
		return f.closeErr
	}
	return nil
}

func (f *fakeInstrumentation) Name() string {
	return f.name
}

func (f *fakeInstrumentation) RequiredVariables() []InstrumentationVariableName {
	if len(f.requires) == 0 {
		return nil
	}
	out := make([]InstrumentationVariableName, len(f.requires))
	copy(out, f.requires)
	return out
}

func (f *fakeInstrumentation) Running() error {
	if !f.started {
		return errors.New("instrumentation not started")
	}
	if f.running {
		return nil
	}
	if f.runError != nil {
		return f.runError
	}
	return errors.New("instrumentation stopped")
}
