package analysis

import (
	"testing"
)

func TestDetectArchitectureFromDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc string
		want string
	}{
		{
			desc: "ELF 64-bit LSB executable, x86-64, version 1 (SYSV)",
			want: "x86_64",
		},
		{
			desc: "ELF 32-bit LSB executable, Intel 80386, version 1 (SYSV)",
			want: "x86",
		},
		{
			desc: "ELF 64-bit LSB executable, ARM aarch64, version 1 (SYSV)",
			want: "arm64",
		},
		{
			desc: "ELF 32-bit LSB executable, ARM, EABI5 version 1 (SYSV)",
			want: "arm",
		},
		{
			desc: "ELF 64-bit MSB executable, MIPS, MIPS64 rel2 version 1 (SYSV)",
			want: "mips64",
		},
		{
			desc: "ELF 64-bit MSB executable, 64-bit PowerPC or cisco 7500, version 1",
			want: "ppc64",
		},
		{
			desc: "data",
			want: "",
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
		want   string
	}{
		{"amd64", "x86_64"},
		{"386", "x86"},
		{"arm64", "arm64"},
		{"arm", "arm"},
		{"mips64", "mips64"},
		{"mips", "mips"},
		{"ppc64", "ppc64"},
		{"ppc64le", "ppc64"},
		{"ppc", "ppc"},
		{"s390x", "s390x"},
		{"wasm", ""},
	}

	for _, tt := range tests {
		if got := hostArchitectureFor(tt.goarch); got != tt.want {
			t.Fatalf("hostArchitectureFor(%q) = %q, want %q", tt.goarch, got, tt.want)
		}
	}
}
