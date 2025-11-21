package analysis

import (
	"testing"

	"github.com/cochaviz/bottle/arch"
)

func TestDetectArchitectureFromDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc string
		want arch.Architecture
	}{
		{
			desc: "ELF 64-bit LSB executable, x86-64, version 1 (SYSV)",
			want: arch.X86_64,
		},
		{
			desc: "ELF 32-bit LSB executable, Intel 80386, version 1 (SYSV)",
			want: arch.I686,
		},
		{
			desc: "ELF 64-bit LSB executable, ARM aarch64, version 1 (SYSV)",
			want: arch.AArch64,
		},
		{
			desc: "ELF 32-bit LSB executable, ARM, EABI5 version 1 (SYSV)",
			want: arch.ARMV7L,
		},
		{
			desc: "ELF 64-bit MSB executable, MIPS, MIPS64 rel2 version 1 (SYSV)",
			want: arch.MIPS64,
		},
		{
			desc: "ELF 64-bit MSB executable, 64-bit PowerPC or cisco 7500, version 1",
			want: arch.PPC64LE,
		},
		{
			desc: "data",
			want: arch.Architecture(""),
		},
	}

	for _, tt := range tests {
		got := detectArchitectureFromDescription(tt.desc)
		if got != tt.want {
			t.Fatalf("detectArchitectureFromDescription(%q) = %q, want %q", tt.desc, got, tt.want)
		}
	}
}

func TestDetermineSampleArchitectureMissingPath(t *testing.T) {
	t.Parallel()

	if _, err := determineSampleArchitecture(Sample{}); err == nil {
		t.Fatal("determineSampleArchitecture() error = nil, want non-nil")
	}
}

func TestIsShellScriptDescription(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc string
		want bool
	}{
		{"POSIX shell script, ASCII text executable", true},
		{"Bourne-Again shell script, UTF-8 text", true},
		{"ELF 64-bit LSB executable, x86-64", false},
		{"", false},
	}

	for _, tt := range cases {
		if got := isShellScriptDescription(tt.desc); got != tt.want {
			t.Fatalf("isShellScriptDescription(%q) = %v, want %v", tt.desc, got, tt.want)
		}
	}
}

func TestHostArchitectureFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		goarch string
		want   arch.Architecture
	}{
		{"amd64", arch.X86_64},
		{"386", arch.I686},
		{"arm64", arch.AArch64},
		{"arm", arch.ARMV7L},
		{"mips64", arch.MIPS64},
		{"mips", arch.MIPS},
		{"ppc64", arch.PPC64LE},
		{"ppc64le", arch.PPC64LE},
		{"ppc", arch.Architecture("")},
		{"s390x", arch.S390X},
		{"wasm", arch.Architecture("")},
	}

	for _, tt := range tests {
		if got := hostArchitectureFor(tt.goarch); got != tt.want {
			t.Fatalf("hostArchitectureFor(%q) = %q, want %q", tt.goarch, got, tt.want)
		}
	}
}
