package analysis

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/cochaviz/bottle/internal/sandbox"
	"gopkg.in/yaml.v3"
)

type InstrumentationVariableName = string

const (
	InstrumentationC2Address   InstrumentationVariableName = "C2Ip"
	InstrumentationVMIP        InstrumentationVariableName = "VmIp"
	InstrumentationVMInterface InstrumentationVariableName = "VmInterface"
	InstrumentationSampleName  InstrumentationVariableName = "SampleName"
)

type cliInstrumentationConfigList []*CLIInstrumentationConfig

func (l *cliInstrumentationConfigList) UnmarshalYAML(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.SequenceNode {
		var items []*CLIInstrumentationConfig
		if err := node.Decode(&items); err != nil {
			return err
		}
		*l = items
		return nil
	}
	var single CLIInstrumentationConfig
	if err := node.Decode(&single); err != nil {
		return err
	}
	*l = []*CLIInstrumentationConfig{&single}
	return nil
}

type suricataInstrumentationConfigList []*SuricataInstrumentationConfig

func (l *suricataInstrumentationConfigList) UnmarshalYAML(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.SequenceNode {
		var items []*SuricataInstrumentationConfig
		if err := node.Decode(&items); err != nil {
			return err
		}
		*l = items
		return nil
	}
	var single SuricataInstrumentationConfig
	if err := node.Decode(&single); err != nil {
		return err
	}
	*l = []*SuricataInstrumentationConfig{&single}
	return nil
}

type instrumentationConfig struct {
	CLI      cliInstrumentationConfigList      `yaml:"cli"`
	Suricata suricataInstrumentationConfigList `yaml:"suricata"`
}

type InstrumentationVariable struct {
	Name  InstrumentationVariableName
	Value string
}

type Instrumentation interface {
	// Start starts the instrumentation with the given variables.
	Start(ctx context.Context, lease sandbox.SandboxLease, variables ...InstrumentationVariable) error

	// Close closes the instrumentation.
	Close() error
}

type InstrumentationConfig interface {
	Parse() (Instrumentation, error)
}

func instrumentationTemplateData(variables []InstrumentationVariable) map[string]string {
	data := map[string]string{}
	for _, variable := range variables {
		data[variable.Name] = variable.Value
	}
	return data
}

func instrumentationVariableValue(variables []InstrumentationVariable, name InstrumentationVariableName) string {
	for _, v := range variables {
		if v.Name == name {
			return strings.TrimSpace(v.Value)
		}
	}
	return ""
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

func LoadInstrumentation(path string) ([]Instrumentation, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg = &instrumentationConfig{}
	instrumentations := []Instrumentation{}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal instrumentation config: %w", err)
	}
	if len(cfg.CLI) > 0 {
		for _, entry := range cfg.CLI {
			if entry == nil {
				continue
			}
			if s := strings.TrimSpace(entry.Command); s != "" {
				instrumentation, err := NewCommandLineInstrumentation(s)
				if err != nil {
					return nil, err
				}
				instrumentations = append(instrumentations, instrumentation)
			} else {
				return nil, fmt.Errorf("Command in instrumentation cannot be empty")
			}
		}
	}
	if len(cfg.Suricata) > 0 {
		for _, entry := range cfg.Suricata {
			if entry == nil {
				continue
			}
			if strings.TrimSpace(entry.Config) == "" {
				continue
			}
			instrumentation, err := NewSuricataInstrumentation(entry.Config, entry.Binary)
			if err != nil {
				return nil, err
			}
			instrumentations = append(instrumentations, instrumentation)
		}
	}
	return instrumentations, nil
}
