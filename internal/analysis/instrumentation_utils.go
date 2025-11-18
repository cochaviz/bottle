package analysis

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/cochaviz/bottle/internal/sandbox"
)

func instrumentationWorkingDir(lease sandbox.SandboxLease, variables []InstrumentationVariable) string {
	dir := strings.TrimSpace(instrumentationVariableValue(variables, InstrumentationLogDir))
	if dir == "" {
		if leaseDir := strings.TrimSpace(lease.RunDir); leaseDir != "" {
			dir = leaseDir
		}
	}
	return dir
}

func instrumentationLabelFromCommand(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	base := filepath.Base(fields[0])
	return sanitizeInstrumentationLabel(base)
}

func sanitizeInstrumentationLabel(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-' || r == '_':
			builder.WriteRune(r)
		case r == '/' || r == '\\' || r == ' ' || r == ':' || r == '.':
			builder.WriteRune('-')
		default:
			// drop other characters
		}
	}
	result := strings.Trim(builder.String(), "-_")
	if result == "" {
		return ""
	}
	return result
}

func resolveInstrumentationOutput(value string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(value))
	if mode == "" {
		mode = "stdout"
	}
	switch mode {
	case "stdout", "file":
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported instrumentation output %q", value)
	}
}
