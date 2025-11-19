package analysis

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"github.com/cochaviz/bottle/internal/sandbox"
)

var _ Instrumentation = (*suricataInstrumentation)(nil)

type SuricataInstrumentationConfig struct {
	Config   string                        `yaml:"config"`
	Binary   string                        `yaml:"binary,omitempty"`
	Output   string                        `yaml:"output,omitempty"`
	Requires []InstrumentationVariableName `yaml:"requires"`
}

type suricataInstrumentation struct {
	configTemplate     *template.Template
	binary             string
	outputMode         string
	cancel             context.CancelFunc
	done               chan struct{}
	renderedConfigPath string
	logFile            *os.File
	requires           []InstrumentationVariableName
	name               string
	runErr             error
	mu                 sync.Mutex
}

// NewSuricataInstrumentation loads the specified Suricata config template and returns
// an instrumentation implementation that renders it for each run and starts suricata.
func NewSuricataInstrumentation(cfg *SuricataInstrumentationConfig) (Instrumentation, error) {
	if cfg == nil {
		return nil, errors.New("suricata instrumentation config cannot be nil")
	}
	configPath := strings.TrimSpace(cfg.Config)
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

	binary := strings.TrimSpace(cfg.Binary)
	if binary == "" {
		binary = "suricata"
	}

	outputMode, err := resolveInstrumentationOutput(cfg.Output)
	if err != nil {
		return nil, err
	}

	var requires []InstrumentationVariableName
	for _, item := range cfg.Requires {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		requires = append(requires, item)
	}
	name := filepath.Base(configPath)
	name = sanitizeInstrumentationLabel(name)
	if name == "" {
		name = "suricata"
	}
	return &suricataInstrumentation{
		configTemplate: tmpl,
		binary:         binary,
		outputMode:     outputMode,
		requires:       requires,
		name:           name,
	}, nil
}

func (i *suricataInstrumentation) Start(ctx context.Context, lease sandbox.SandboxLease, variables ...InstrumentationVariable) error {
	if i == nil {
		return nil
	}

	if err := ensureRequiredInstrumentationVariables(variables, i.requires, i.Name()); err != nil {
		return err
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
	dir := strings.TrimSpace(instrumentationVariableValue(variables, InstrumentationLogDir))
	if dir != "" {
		cmd.Dir = dir
	}

	var (
		logFile    *os.File
		stdoutPipe io.ReadCloser
		stderrPipe io.ReadCloser
	)
	if i.outputMode == "file" {
		if dir == "" {
			i.cleanupRenderedConfig()
			cancel()
			return errors.New("log directory unavailable for suricata output")
		}
		stdoutPipe, err = cmd.StdoutPipe()
		if err != nil {
			i.cleanupRenderedConfig()
			cancel()
			return fmt.Errorf("create suricata stdout pipe: %w", err)
		}
		stderrPipe, err = cmd.StderrPipe()
		if err != nil {
			i.cleanupRenderedConfig()
			cancel()
			stdoutPipe.Close()
			return fmt.Errorf("create suricata stderr pipe: %w", err)
		}
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Start(); err != nil {
		if stdoutPipe != nil {
			stdoutPipe.Close()
		}
		if stderrPipe != nil {
			stderrPipe.Close()
		}
		i.cleanupRenderedConfig()
		cancel()
		return fmt.Errorf("start suricata: %w", err)
	}

	if stdoutPipe != nil && stderrPipe != nil {
		logPath := filepath.Join(dir, fmt.Sprintf("suricata-%d.log", cmd.Process.Pid))
		logFile, err = os.Create(logPath)
		if err != nil {
			stdoutPipe.Close()
			stderrPipe.Close()
			i.cleanupRenderedConfig()
			cancel()
			return fmt.Errorf("create suricata log file: %w", err)
		}
		i.logFile = logFile
		go func() {
			defer stdoutPipe.Close()
			_, _ = io.Copy(logFile, stdoutPipe)
		}()
		go func() {
			defer stderrPipe.Close()
			_, _ = io.Copy(logFile, stderrPipe)
		}()
	}

	i.setRunErr(nil)
	i.done = make(chan struct{})
	go func() {
		err := cmd.Wait()
		i.setRunErr(err)
		close(i.done)
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
		<-i.done
		if err := instrumentationCloseError(i.getRunErr()); err != nil {
			i.cleanupRenderedConfig()
			return err
		}
		i.done = nil
	}
	if i.logFile != nil {
		_ = i.logFile.Close()
		i.logFile = nil
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

func (i *suricataInstrumentation) Name() string {
	if i == nil {
		return ""
	}
	if i.name == "" {
		return "suricata"
	}
	return i.name
}

func (i *suricataInstrumentation) RequiredVariables() []InstrumentationVariableName {
	if i == nil || len(i.requires) == 0 {
		return nil
	}
	result := make([]InstrumentationVariableName, len(i.requires))
	copy(result, i.requires)
	return result
}

func (i *suricataInstrumentation) Running() error {
	if i == nil || i.done == nil {
		return nil
	}
	select {
	case <-i.done:
		if err := instrumentationCloseError(i.getRunErr()); err != nil {
			return err
		}
		return errors.New("instrumentation process exited")
	default:
		return nil
	}
}

func (i *suricataInstrumentation) setRunErr(err error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.runErr = err
}

func (i *suricataInstrumentation) getRunErr() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.runErr
}
