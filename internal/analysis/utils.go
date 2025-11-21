package analysis

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/cochaviz/bottle/arch"
)

func determineSampleArchitecture(sample Sample) (arch.Architecture, error) {
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

func detectArchitectureFromDescription(desc string) arch.Architecture {
	if desc == "" {
		return ""
	}

	lower := strings.ToLower(desc)
	switch {
	case strings.Contains(lower, "x86-64"), strings.Contains(lower, "x86_64"), strings.Contains(lower, "amd64"):
		return arch.X86_64
	case strings.Contains(lower, "80386"), strings.Contains(lower, "i386"), strings.Contains(lower, "x86"):
		return arch.I686
	case strings.Contains(lower, "aarch64"), strings.Contains(lower, "arm64"):
		return arch.AArch64
	case strings.Contains(lower, "arm"):
		return arch.ARMV7L
	case strings.Contains(lower, "mips64"):
		return arch.MIPS64
	case strings.Contains(lower, "mips"):
		return arch.MIPS
	case strings.Contains(lower, "powerpc64"), strings.Contains(lower, "ppc64"), (strings.Contains(lower, "powerpc") && strings.Contains(lower, "64-bit")):
		return arch.PPC64LE
	case strings.Contains(lower, "powerpc"), strings.Contains(lower, "ppc"):
		return arch.PPC64LE
	case strings.Contains(lower, "s390x"), strings.Contains(lower, "system/390"):
		return arch.S390X
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

func hostArchitecture() arch.Architecture {
	return hostArchitectureFor(runtime.GOARCH)
}

func hostArchitectureFor(goarch string) arch.Architecture {
	switch goarch {
	case "amd64":
		return arch.X86_64
	case "386":
		return arch.I686
	case "arm64":
		return arch.AArch64
	case "arm":
		return arch.ARMV7L
	case "mips":
		return arch.MIPS
	case "mips64":
		return arch.MIPS64
	case "ppc64", "ppc64le":
		return arch.PPC64LE
	case "s390x":
		return arch.S390X
	default:
		return ""
	}
}
