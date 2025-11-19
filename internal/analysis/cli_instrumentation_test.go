package analysis

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestBlockCommand(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "instr.yaml")
	config := `cli:
- command: |
    echo "This is \\
    a multi-line command"
`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
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

func TestCommandLineInstrumentationRequiresVariables(t *testing.T) {
	t.Parallel()

	cfg := &CLIInstrumentationConfig{
		Command:  "echo instrumentation",
		Requires: []InstrumentationVariableName{InstrumentationC2Address},
	}
	inst, err := NewCommandLineInstrumentation(cfg)
	if err != nil {
		t.Fatalf("NewCommandLineInstrumentation() error = %v", err)
	}

	ctx := context.Background()

	if err := inst.Start(ctx, sandbox.SandboxLease{}); err == nil {
		t.Fatal("Start() error = nil, want MissingRequiredVariablesError")
	} else {
		var miss *MissingRequiredVariablesError
		if !errors.As(err, &miss) {
			t.Fatalf("Start() error = %v, want MissingRequiredVariablesError", err)
		}
	}

	vars := []InstrumentationVariable{
		{Name: InstrumentationC2Address, Value: "203.0.113.2"},
	}
	if err := inst.Start(ctx, sandbox.SandboxLease{}, vars...); err != nil {
		t.Fatalf("Start() with required vars error = %v", err)
	}
	if err := inst.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestCommandLineInstrumentationMultiLineCommand(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "instr.yaml")
	logDir := t.TempDir()
	config := `cli:
- command: |
    echo "hey" >> "{{ .LogDir }}/multiline.log"
    echo "I am" >> "{{ .LogDir }}/multiline.log"
    echo "multiline" >> "{{ .LogDir }}/multiline.log"
`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	insts, err := LoadInstrumentation(configPath)
	if err != nil {
		t.Fatalf("LoadInstrumentation() error = %v", err)
	}
	if len(insts) != 1 {
		t.Fatalf("LoadInstrumentation() != 1 instrumentation, got %d", len(insts))
	}

	inst := insts[0]
	vars := []InstrumentationVariable{
		{Name: InstrumentationLogDir, Value: logDir},
	}

	if err := inst.Start(context.Background(), sandbox.SandboxLease{}, vars...); err != nil {
		t.Fatalf("instrumentation start error = %v", err)
	}

	logPath := filepath.Join(logDir, "multiline.log")
	var data []byte
	var readErr error
	deadline := time.Now().Add(time.Second)
	for {
		data, readErr = os.ReadFile(logPath)
		if readErr == nil {
			break
		}
		if !os.IsNotExist(readErr) || time.Now().After(deadline) {
			t.Fatalf("read multiline log: %v", readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	want := "hey\nI am\nmultiline\n"
	if string(data) != want {
		t.Fatalf("unexpected multiline log content: %q, want %q", string(data), want)
	}
	if err := inst.Close(); err != nil {
		t.Fatalf("instrumentation close error = %v", err)
	}
}
