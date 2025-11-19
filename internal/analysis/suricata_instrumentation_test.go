package analysis

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cochaviz/bottle/internal/sandbox"
)

func TestSuricataInstrumentation(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "suricata.yaml")
	configContent := `
sensor-name: "{{ .SampleName }}"
vars:
  vm: "{{ .VmIp }}"
  iface: "{{ .VmInterface }}"
  c2: "{{ .C2Ip }}"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("write suricata config template: %v", err)
	}

	runnerPath := filepath.Join(tmpDir, "runner.sh")
	if err := os.WriteFile(runnerPath, []byte("#!/bin/sh\nsleep 60\n"), 0o755); err != nil {
		t.Fatalf("write runner: %v", err)
	}

	instrPath := filepath.Join(tmpDir, "inst.yaml")
	instConfig := []byte("suricata:\n  config: " + configPath + "\n  binary: " + runnerPath + "\n")
	if err := os.WriteFile(instrPath, instConfig, 0o644); err != nil {
		t.Fatalf("write instrumentation config: %v", err)
	}

	insts, err := LoadInstrumentation(instrPath)
	if err != nil {
		t.Fatalf("LoadInstrumentation() error = %v", err)
	}
	if len(insts) > 1 {
		t.Fatalf("Only defined a single instrumentation, but multiple parsed")
	}

	vars := []InstrumentationVariable{
		{Name: InstrumentationSampleName, Value: "beacon.bin"},
		{Name: InstrumentationVMIP, Value: "10.13.37.50"},
		{Name: InstrumentationVMInterface, Value: "veth-sample"},
		{Name: InstrumentationC2Address, Value: "10.66.66.50"},
	}

	inst := insts[0]

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := inst.Start(ctx, sandbox.SandboxLease{}, vars...); err != nil {
		t.Fatalf("suricata instrumentation start error = %v", err)
	}

	suricataInst, ok := inst.(*suricataInstrumentation)
	if !ok {
		t.Fatal("expected suricata instrumentation type")
	}
	if suricataInst.renderedConfigPath == "" {
		t.Fatal("rendered config path is empty")
	}
	data, err := os.ReadFile(suricataInst.renderedConfigPath)
	if err != nil {
		t.Fatalf("read rendered config: %v", err)
	}
	if !strings.Contains(string(data), "beacon.bin") {
		t.Fatalf("rendered config missing sensor name: %s", data)
	}
	if !strings.Contains(string(data), "veth-sample") {
		t.Fatalf("rendered config missing vm interface: %s", data)
	}

	if err := inst.Close(); err != nil {
		t.Fatalf("suricata instrumentation close error = %v", err)
	}
}

func TestSuricataInstrumentationRequiresVariables(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "suricata.yaml")
	configContent := `
sensor-name: "{{ .SampleName }}"
vars:
  vm: "{{ .VmIp }}"
  iface: "{{ .VmInterface }}"
  c2: "{{ .C2Ip }}"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("write suricata config template: %v", err)
	}

	runnerPath := filepath.Join(tmpDir, "runner.sh")
	if err := os.WriteFile(runnerPath, []byte("#!/bin/sh\nsleep 60\n"), 0o755); err != nil {
		t.Fatalf("write runner: %v", err)
	}

	cfg := &SuricataInstrumentationConfig{
		Config:   configPath,
		Binary:   runnerPath,
		Requires: []InstrumentationVariableName{InstrumentationC2Address},
	}
	instIface, err := NewSuricataInstrumentation(cfg)
	if err != nil {
		t.Fatalf("NewSuricataInstrumentation() error = %v", err)
	}

	vars := []InstrumentationVariable{
		{Name: InstrumentationSampleName, Value: "beacon.bin"},
		{Name: InstrumentationVMIP, Value: "10.13.37.50"},
		{Name: InstrumentationVMInterface, Value: "veth-sample"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := instIface.Start(ctx, sandbox.SandboxLease{}, vars...); err == nil {
		t.Fatal("Start() error = nil, want MissingRequiredVariablesError")
	} else {
		var miss *MissingRequiredVariablesError
		if !errors.As(err, &miss) {
			t.Fatalf("Start() error = %v, want MissingRequiredVariablesError", err)
		}
	}

	vars = append(vars, InstrumentationVariable{Name: InstrumentationC2Address, Value: "203.0.113.2"})
	if err := instIface.Start(ctx, sandbox.SandboxLease{}, vars...); err != nil {
		t.Fatalf("Start() with required vars error = %v", err)
	}
	if err := instIface.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
