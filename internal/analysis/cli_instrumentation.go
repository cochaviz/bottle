package analysis

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"github.com/cochaviz/bottle/internal/sandbox"
)

type CommandLineInstrumentation struct {
	template *template.Template
	cancel   context.CancelFunc
	done     chan error
}

type CLIInstrumentationConfig struct {
	Command string `yaml:"command"`
}

func (c *CLIInstrumentationConfig) Parse() (Instrumentation, error) {
	if c == nil {
		return nil, fmt.Errorf("CLIConfig cannot be nil")
	}
	if s := strings.TrimSpace(c.Command); s != "" {
		return NewCommandLineInstrumentation(s)
	} else {
		return nil, fmt.Errorf("Command in instrumentation cannot be empty")
	}
}

func NewCommandLineInstrumentation(commandTemplate string) (*CommandLineInstrumentation, error) {
	commandTemplate = strings.TrimSpace(commandTemplate)
	if commandTemplate == "" {
		return nil, errors.New("command template is required")
	}
	tmpl, err := template.New("instrumentation").Parse(commandTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse instrumentation command template: %w", err)
	}
	return &CommandLineInstrumentation{template: tmpl}, nil
}

func (i *CommandLineInstrumentation) Start(ctx context.Context, lease sandbox.SandboxLease, variables ...InstrumentationVariable) error {
	if i == nil {
		return nil
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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start instrumentation command: %w", err)
	}

	i.cancel = cancel
	i.done = make(chan error, 1)
	go func() {
		i.done <- cmd.Wait()
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
	if err := <-i.done; err != nil {
		if closeErr := instrumentationCloseError(err); closeErr != nil {
			return closeErr
		}
	}
	return nil
}
