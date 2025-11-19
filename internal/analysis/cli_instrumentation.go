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

type CLIInstrumentationConfig struct {
	Command  string                        `yaml:"command"`
	Output   string                        `yaml:"output,omitempty"`
	Requires []InstrumentationVariableName `yaml:"requires"`
}

func (c *CLIInstrumentationConfig) Parse() (Instrumentation, error) {
	if c == nil {
		return nil, fmt.Errorf("CLIConfig cannot be nil")
	}
	return NewCommandLineInstrumentation(c)
}

type CommandLineInstrumentation struct {
	template   *template.Template
	cancel     context.CancelFunc
	done       chan struct{}
	outputMode string
	logFile    *os.File
	requires   []InstrumentationVariableName
	name       string
	runErr     error
	mu         sync.Mutex
}

func NewCommandLineInstrumentation(cfg *CLIInstrumentationConfig) (*CommandLineInstrumentation, error) {
	if cfg == nil {
		return nil, errors.New("cli instrumentation config cannot be nil")
	}
	commandTemplate := strings.TrimSpace(cfg.Command)
	if commandTemplate == "" {
		return nil, errors.New("command template is required")
	}
	tmpl, err := template.New("instrumentation").Parse(commandTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse instrumentation command template: %w", err)
	}
	output, err := resolveInstrumentationOutput(cfg.Output)
	if err != nil {
		return nil, err
	}
	name := instrumentationLabelFromCommand(commandTemplate)
	if name == "" {
		name = "cli"
	}
	var requires []InstrumentationVariableName
	for _, item := range cfg.Requires {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		requires = append(requires, item)
	}
	return &CommandLineInstrumentation{
		template:   tmpl,
		outputMode: output,
		requires:   requires,
		name:       name,
	}, nil
}

func (i *CommandLineInstrumentation) Start(ctx context.Context, lease sandbox.SandboxLease, variables ...InstrumentationVariable) error {
	if i == nil {
		return nil
	}

	if err := ensureRequiredInstrumentationVariables(variables, i.requires, i.Name()); err != nil {
		return err
	}

	data := instrumentationTemplateData(variables)
	var rendered bytes.Buffer
	if err := i.template.Execute(&rendered, data); err != nil {
		return fmt.Errorf("render instrumentation command: %w", err)
	}

	command := strings.TrimSpace(rendered.String())
	if command == "" {
		return errors.New("instrumentation command rendered empty")
	}

	if i.cancel != nil {
		i.cancel()
	}

	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, "sh", "-c", command)

	dir := strings.TrimSpace(instrumentationVariableValue(variables, InstrumentationLogDir))
	if dir != "" {
		cmd.Dir = dir
	}

	var (
		logFile    *os.File
		stdoutPipe io.ReadCloser
		stderrPipe io.ReadCloser
		label      string
		err        error
	)
	if i.outputMode == "file" {
		if dir == "" {
			cancel()
			return errors.New("log directory is not available for instrumentation file output")
		}
		stdoutPipe, err = cmd.StdoutPipe()
		if err != nil {
			cancel()
			return fmt.Errorf("create stdout pipe: %w", err)
		}
		stderrPipe, err = cmd.StderrPipe()
		if err != nil {
			cancel()
			stdoutPipe.Close()
			return fmt.Errorf("create stderr pipe: %w", err)
		}
		label = instrumentationLabelFromCommand(command)
		if label == "" {
			label = "cli"
		}
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Start(); err != nil {
		cancel()
		if stdoutPipe != nil {
			stdoutPipe.Close()
		}
		if stderrPipe != nil {
			stderrPipe.Close()
		}
		return fmt.Errorf("start instrumentation command: %w", err)
	}

	if stdoutPipe != nil && stderrPipe != nil {
		logPath := filepath.Join(dir, fmt.Sprintf("%s-%d.log", label, cmd.Process.Pid))
		logFile, err = os.Create(logPath)
		if err != nil {
			stdoutPipe.Close()
			stderrPipe.Close()
			cancel()
			return fmt.Errorf("create instrumentation log file: %w", err)
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

	i.cancel = cancel
	i.setRunErr(nil)
	i.done = make(chan struct{})
	go func() {
		err := cmd.Wait()
		i.setRunErr(err)
		close(i.done)
	}()
	return nil
}

func (i *CommandLineInstrumentation) Close() error {
	if i == nil {
		return nil
	}
	if i.cancel != nil {
		i.cancel()
	}
	if i.done == nil {
		return nil
	}
	<-i.done
	if err := instrumentationCloseError(i.getRunErr()); err != nil {
		return err
	}
	if i.logFile != nil {
		_ = i.logFile.Close()
		i.logFile = nil
	}
	return nil
}

func (i *CommandLineInstrumentation) Name() string {
	if i == nil {
		return ""
	}
	if i.name == "" {
		return "cli"
	}
	return i.name
}

func (i *CommandLineInstrumentation) RequiredVariables() []InstrumentationVariableName {
	if i == nil || len(i.requires) == 0 {
		return nil
	}
	result := make([]InstrumentationVariableName, len(i.requires))
	copy(result, i.requires)
	return result
}

func (i *CommandLineInstrumentation) Running() error {
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

func (i *CommandLineInstrumentation) setRunErr(err error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.runErr = err
}

func (i *CommandLineInstrumentation) getRunErr() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.runErr
}
