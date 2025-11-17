package analysis

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

func determineSampleArchitecture(sample Sample) (string, error) {
	path := strings.TrimSpace(sample.Artifact)
	if path == "" {
		return "", errors.New("sample artifact path is required")
	}

	output, err := exec.Command("file", "-b", path).CombinedOutput()
	desc := strings.TrimSpace(string(output))
	if err != nil {
		return "", fmt.Errorf("file command failed: %w (output: %s)", err, desc)
	}

	if isShellScriptDescription(desc) {
		if arch := hostArchitecture(); arch != "" {
			return arch, nil
		}
		return "", errors.New("shell script detected but host architecture is unsupported")
	}

	if arch := detectArchitectureFromDescription(desc); arch != "" {
		return arch, nil
	}
	return "", fmt.Errorf("unable to determine architecture from description: %s", desc)
}

func detectArchitectureFromDescription(desc string) string {
	if desc == "" {
		return ""
	}

	lower := strings.ToLower(desc)
	switch {
	case strings.Contains(lower, "x86-64"), strings.Contains(lower, "x86_64"), strings.Contains(lower, "amd64"):
		return "x86_64"
	case strings.Contains(lower, "80386"), strings.Contains(lower, "i386"), strings.Contains(lower, "x86"):
		return "x86"
	case strings.Contains(lower, "aarch64"), strings.Contains(lower, "arm64"):
		return "arm64"
	case strings.Contains(lower, "arm"):
		return "arm"
	case strings.Contains(lower, "mips64"):
		return "mips64"
	case strings.Contains(lower, "mips"):
		return "mips"
	case strings.Contains(lower, "powerpc64"), strings.Contains(lower, "ppc64"), (strings.Contains(lower, "powerpc") && strings.Contains(lower, "64-bit")):
		return "ppc64"
	case strings.Contains(lower, "powerpc"), strings.Contains(lower, "ppc"):
		return "ppc"
	case strings.Contains(lower, "s390x"), strings.Contains(lower, "system/390"):
		return "s390x"
	default:
		return ""
	}
}

func isShellScriptDescription(desc string) bool {
	if desc == "" {
		return false
	}
	lower := strings.ToLower(desc)
	return strings.Contains(lower, "shell script")
}

func hostArchitecture() string {
	return hostArchitectureFor(runtime.GOARCH)
}

func hostArchitectureFor(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "386":
		return "x86"
	case "arm64":
		return "arm64"
	case "arm":
		return "arm"
	case "mips":
		return "mips"
	case "mips64":
		return "mips64"
	case "ppc64", "ppc64le":
		return "ppc64"
	case "ppc":
		return "ppc"
	case "s390x":
		return "s390x"
	default:
		return ""
	}
}
