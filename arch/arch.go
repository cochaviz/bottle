package arch

import (
	"fmt"
	"sort"
	"strings"
)

// Architecture defines the set of values accepted by qemu/libvirt.
type Architecture string

const (
	X86_64  Architecture = "x86_64"
	I686    Architecture = "i686"
	AArch64 Architecture = "aarch64"
	ARMV7L  Architecture = "armv7l"
	PPC64LE Architecture = "ppc64le"
	S390X   Architecture = "s390x"
	MIPS    Architecture = "mips"
	MIPSEL  Architecture = "mipsel"
	MIPS64  Architecture = "mips64"
)

// Supported returns the full list of supported architectures.
func Supported() []Architecture {
	return []Architecture{
		X86_64,
		I686,
		AArch64,
		ARMV7L,
		PPC64LE,
		S390X,
		MIPS,
		MIPSEL,
		MIPS64,
	}
}

// IsValid reports whether a matches a supported architecture value.
func (a Architecture) IsValid() bool {
	switch a {
	case X86_64, I686, AArch64, ARMV7L, PPC64LE, S390X, MIPS, MIPSEL, MIPS64:
		return true
	default:
		return false
	}
}

// String returns the architecture as string.
func (a Architecture) String() string {
	return string(a)
}

// Parse returns the canonical Architecture for the provided string or an error if unsupported.
func Parse(value string) (Architecture, error) {
	if arch := Normalize(value); arch != "" {
		return arch, nil
	}
	return "", fmt.Errorf("unsupported architecture %q (supported: %s)", value, strings.Join(supportedStrings(), ", "))
}

// MustParse is like Parse but panics on error.
func MustParse(value string) Architecture {
	arch, err := Parse(value)
	if err != nil {
		panic(err)
	}
	return arch
}

// Normalize maps a possibly ambiguous string into a canonical Architecture. Returns ""
// when the string cannot be normalized.
func Normalize(value string) Architecture {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case string(X86_64), "x86-64", "amd64":
		return X86_64
	case "x86", "i386", "i486", "i586", string(I686), "386", "80386":
		return I686
	case string(AArch64), "arm64":
		return AArch64
	case string(ARMV7L), "arm", "armv7", "armhf":
		return ARMV7L
	case string(PPC64LE), "ppc64", "ppc64el", "powerpc64", "powerpc64le":
		return PPC64LE
	case string(S390X):
		return S390X
	case string(MIPS64), "mips64el":
		return MIPS64
	case string(MIPS):
		return MIPS
	case "mipsel":
		return MIPSEL
	default:
		return ""
	}
}

func supportedStrings() []string {
	all := Supported()
	out := make([]string, 0, len(all))
	for _, a := range all {
		out = append(out, a.String())
	}
	sort.Strings(out)
	return out
}
