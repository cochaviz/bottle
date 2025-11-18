package analysis

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cochaviz/bottle/internal/sandbox"
)

func TestLoadInstrumentation(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "instr.yaml")
	if err := os.WriteFile(configPath, []byte("cli:\n  command: \"echo instrumentation\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	insts, err := LoadInstrumentation(configPath)
	if err != nil {
		t.Fatalf("LoadInstrumentation() error = %v", err)
	}
	if insts == nil {
		t.Fatal("LoadInstrumentation() = nil, want instrumentation")
	}

	if len(insts) > 1 {
		t.Fatal("Only one instrumentation defined")
	}

	inst := insts[0]

	if err := inst.Start(context.Background(), sandbox.SandboxLease{}); err != nil {
		t.Fatalf("instrumentation start error = %v", err)
	}
	if err := inst.Close(); err != nil {
		t.Fatalf("instrumentation close error = %v", err)
	}
}

func TestLoadInstrumentationMissingCommand(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "instr.yaml")
	if err := os.WriteFile(configPath, []byte("cli:\n  command: \"\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := LoadInstrumentation(configPath); err == nil {
		t.Fatal("LoadInstrumentation() error = nil, want non-nil")
	}
}
