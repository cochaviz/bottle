package analysis

import (
	"cochaviz/mime/internal/sandbox"
	"context"
	"os/exec"
)

type CommandLineInstrumentation struct {
	command string
}

func (i *CommandLineInstrumentation) Start(ctx context.Context, lease sandbox.SandboxLease, variables ...InstrumentationVariable) error {
	for _, variable := range variables {
		i.command = findReplaceEscaped(i.command, variable.Name, variable.Value)
	}

	exec.Command(i.command)

	return nil
}

// findReplaceEscaped finds and replaces an escaped pattern (surrounded by
// backticks) in a command string with an escaped replacement string.
func findReplaceEscaped(command string, pattern string, replacement string) string {
	return command
}
