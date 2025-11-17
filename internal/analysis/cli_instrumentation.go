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

	"cochaviz/mime/internal/sandbox"

	"gopkg.in/yaml.v3"
)

type CommandLineInstrumentation struct {
	template *template.Template
	cancel   context.CancelFunc
	done     chan error
}

type instrumentationConfig struct {
	CLI *struct {
		Command string `yaml:"command"`
	} `yaml:"cli"`
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

func LoadInstrumentation(path string) (Instrumentation, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg instrumentationConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal instrumentation config: %w", err)
	}
	if cfg.CLI == nil || strings.TrimSpace(cfg.CLI.Command) == "" {
		return nil, errors.New("instrumentation cli.command is required")
	}
	return NewCommandLineInstrumentation(cfg.CLI.Command)
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

func instrumentationTemplateData(variables []InstrumentationVariable) map[string]string {
	data := map[string]string{}
	for _, variable := range variables {
		data[variable.Name] = variable.Value
	}
	return data
}

func instrumentationCloseError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	if strings.Contains(err.Error(), "killed") {
		return nil
	}
	return fmt.Errorf("instrumentation command exited: %w", err)
}
