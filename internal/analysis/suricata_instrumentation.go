package analysis

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/cochaviz/bottle/internal/sandbox"
)

var _ Instrumentation = (*suricataInstrumentation)(nil)

type SuricataInstrumentationConfig struct {
	Config string `yaml:"config"`
	Binary string `yaml:"binary"`
}

type suricataInstrumentation struct {
	configTemplate     *template.Template
	binary             string
	cancel             context.CancelFunc
	done               chan error
	renderedConfigPath string
}

// NewSuricataInstrumentation loads the specified Suricata config template and returns
// an instrumentation implementation that renders it for each run and starts suricata.
func NewSuricataInstrumentation(configPath, binary string) (Instrumentation, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil, errors.New("suricata instrumentation config path is required")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read suricata config: %w", err)
	}

	tmpl, err := template.New(filepath.Base(configPath)).
		Option("missingkey=zero").
		Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse suricata config template: %w", err)
	}

	binary = strings.TrimSpace(binary)
	if binary == "" {
		binary = "suricata"
	}
	return &suricataInstrumentation{
		configTemplate: tmpl,
		binary:         binary,
	}, nil
}

func (i *suricataInstrumentation) Start(ctx context.Context, _ sandbox.SandboxLease, variables ...InstrumentationVariable) error {
	if i == nil {
		return nil
	}

	vmInterface := instrumentationVariableValue(variables, InstrumentationVMInterface)
	if vmInterface == "" {
		return errors.New("suricata instrumentation requires vm_interface data")
	}

	var rendered bytes.Buffer
	if err := i.configTemplate.Execute(&rendered, instrumentationTemplateData(variables)); err != nil {
		return fmt.Errorf("render suricata config: %w", err)
	}

	tmp, err := os.CreateTemp("", "suricata-config-*.yaml")
	if err != nil {
		return fmt.Errorf("create rendered suricata config: %w", err)
	}
	if _, err := tmp.Write(rendered.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return fmt.Errorf("write suricata config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("close suricata config: %w", err)
	}

	i.cleanupRenderedConfig()
	i.renderedConfigPath = tmp.Name()

	if i.cancel != nil {
		i.cancel()
	}
	procCtx, cancel := context.WithCancel(ctx)
	i.cancel = cancel

	cmd := exec.CommandContext(procCtx, i.binary, "-c", i.renderedConfigPath, "-i", vmInterface)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		i.cleanupRenderedConfig()
		cancel()
		return fmt.Errorf("start suricata: %w", err)
	}

	i.done = make(chan error, 1)
	go func() {
		i.done <- cmd.Wait()
	}()
	return nil
}

func (i *suricataInstrumentation) Close() error {
	if i == nil {
		return nil
	}
	if i.cancel != nil {
		i.cancel()
	}
	if i.done != nil {
		if err := <-i.done; err != nil {
			if closeErr := instrumentationCloseError(err); closeErr != nil {
				i.cleanupRenderedConfig()
				return closeErr
			}
		}
		i.done = nil
	}
	i.cleanupRenderedConfig()
	return nil
}

func (i *suricataInstrumentation) cleanupRenderedConfig() {
	if i.renderedConfigPath == "" {
		return
	}
	_ = os.Remove(i.renderedConfigPath)
	i.renderedConfigPath = ""
}
