package libvirt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"cochaviz/mime/internal/build"
	"cochaviz/mime/internal/sandbox"
)

func TestDeriveConfigPopulatesFields(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	networkPath := filepath.Join(tempDir, "network.xml")

	machine := "pc-q35-8.1"
	ctx := build.BuildContext{
		Spec: build.BuildSpecification{
			Profile: build.BuildProfile{
				Console:        "ttyS0",
				KernelURL:      "http://example.test/kernel",
				InitrdURL:      "http://example.test/initrd",
				MirrorHost:     "mirror.example.test",
				MirrorPath:     "/debian",
				Release:        "bookworm",
				DiskSizeGB:     25,
				NetworkName:    "default",
				PreseedEnabled: false,
			},
			SandboxSpecification: sandbox.SandboxSpecification{
				DomainProfile: sandbox.DomainProfile{
					Arch:         "amd64",
					Machine:      &machine,
					VCPUs:        4,
					RAMMB:        8192,
					NetworkModel: "virtio",
					ExtraArgs:    []string{"console=ttyS0"},
				},
			},
		},
	}

	env := LibvirtBuildEnvironment{
		WorkDir:                  tempDir,
		NetworkConfigurationPath: &networkPath,
	}

	cfg, err := deriveConfig(ctx, env)
	if err != nil {
		t.Fatalf("deriveConfig() error = %v", err)
	}

	if cfg.OutputDir != tempDir {
		t.Fatalf("unexpected output dir: got %q want %q", cfg.OutputDir, tempDir)
	}

	expectedOutput := filepath.Join(tempDir, "sandbox-bookworm-x86_64.qcow2")
	if cfg.OutputPath != expectedOutput {
		t.Fatalf("unexpected output path: got %q want %q", cfg.OutputPath, expectedOutput)
	}

	if cfg.ConnectURI != env.ConnectURI {
		t.Fatalf("unexpected connect uri: got %q want %q", cfg.ConnectURI, env.ConnectURI)
	}

	if cfg.NetworkConfigurationPath == nil || *cfg.NetworkConfigurationPath != networkPath {
		t.Fatalf("expected network config path to be propagated")
	}

	if cfg.HostArch != canonicalizeArch(runtime.GOARCH) {
		t.Fatalf("unexpected host arch: got %q want %q", cfg.HostArch, canonicalizeArch(runtime.GOARCH))
	}

	if len(cfg.ExtraArgs) != len(ctx.Spec.SandboxSpecification.DomainProfile.ExtraArgs) {
		t.Fatalf("expected extra args to be copied, got %d args", len(cfg.ExtraArgs))
	}

	if cfg.PreseedPath != nil {
		t.Fatalf("expected nil preseed path when preseed disabled")
	}

	expectedDomain := "sandbox-builder-bookworm-x86_64"
	if cfg.DomainName != expectedDomain {
		t.Fatalf("unexpected domain name: got %q want %q", cfg.DomainName, expectedDomain)
	}
}

func TestDeriveConfigValidatesPreseedPresence(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	preseedPath := filepath.Join(tempDir, "preseed.cfg")

	ctx := build.BuildContext{
		Spec: build.BuildSpecification{
			SandboxSpecification: sandbox.SandboxSpecification{
				DomainProfile: sandbox.DomainProfile{
					Arch:         "amd64",
					NetworkModel: "virtio",
				},
			},
			Profile: build.BuildProfile{
				Console:        "ttyS0",
				MirrorHost:     "mirror",
				MirrorPath:     "/mirror",
				KernelURL:      "http://example.test/kernel",
				InitrdURL:      "http://example.test/initrd",
				Release:        "bookworm",
				DiskSizeGB:     10,
				NetworkName:    "default",
				PreseedEnabled: true,
			},
		},
	}

	env := LibvirtBuildEnvironment{
		WorkDir:           tempDir,
		PreseedConfigPath: &preseedPath,
	}

	_, err := deriveConfig(ctx, env)
	if err == nil {
		t.Fatalf("deriveConfig() error = nil, want error")
	}

	var buildErr *build.BuildError
	if !errors.As(err, &buildErr) {
		t.Fatalf("expected build error, got %T", err)
	}
}

func TestDeriveConfigHandlesPreseedFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	preseedPath := filepath.Join(tempDir, "preseed.cfg")
	if err := os.WriteFile(preseedPath, []byte("# preseed"), 0o600); err != nil {
		t.Fatalf("write preseed: %v", err)
	}

	ctx := build.BuildContext{
		Spec: build.BuildSpecification{
			Profile: build.BuildProfile{
				Console:        "ttyS0",
				MirrorHost:     "mirror",
				MirrorPath:     "/mirror",
				KernelURL:      "http://example.test/kernel",
				InitrdURL:      "http://example.test/initrd",
				Release:        "bookworm",
				DiskSizeGB:     10,
				NetworkName:    "default",
				PreseedEnabled: true,
			},
			SandboxSpecification: sandbox.SandboxSpecification{
				DomainProfile: sandbox.DomainProfile{
					Arch:         "amd64",
					NetworkModel: "virtio",
				},
			},
		},
	}

	env := LibvirtBuildEnvironment{
		WorkDir:           tempDir,
		PreseedConfigPath: &preseedPath,
	}

	cfg, err := deriveConfig(ctx, env)
	if err != nil {
		t.Fatalf("deriveConfig() error = %v", err)
	}

	if cfg.PreseedPath == nil || *cfg.PreseedPath != preseedPath {
		t.Fatalf("expected preseed path to be propagated")
	}
}

func TestBuildKernelArgsWithPreseed(t *testing.T) {
	t.Parallel()

	preseedPath := filepath.Join("tmp", "preseed.cfg")
	cfg := &BuildConfig{
		UsePreseed:  true,
		PreseedPath: &preseedPath,
		MirrorHost:  "mirror",
		MirrorPath:  "/mirror",
		Console:     "ttyS0",
		ExtraArgs:   []string{"foo=bar"},
	}

	args, err := buildKernelArgs(cfg)
	if err != nil {
		t.Fatalf("buildKernelArgs() error = %v", err)
	}

	if args[0] != fmt.Sprintf("preseed/file=/%s", filepath.Base(preseedPath)) {
		t.Fatalf("unexpected preseed arg: %q", args[0])
	}

	if args[len(args)-1] != "foo=bar" {
		t.Fatalf("expected extra args appended, got %v", args)
	}
}

func TestBuildKernelArgsWithoutPreseed(t *testing.T) {
	t.Parallel()

	cfg := &BuildConfig{
		UsePreseed: false,
		MirrorHost: "mirror",
		MirrorPath: "/mirror",
		Console:    "ttyS0",
	}

	args, err := buildKernelArgs(cfg)
	if err != nil {
		t.Fatalf("buildKernelArgs() error = %v", err)
	}

	for _, arg := range args {
		if arg == "auto=true" || arg == "priority=critical" {
			t.Fatalf("unexpected automated installer arg in %v", args)
		}
	}
}

func TestBuildKernelArgsRequiresPreseedPath(t *testing.T) {
	t.Parallel()

	cfg := &BuildConfig{
		UsePreseed: true,
		MirrorHost: "mirror",
		MirrorPath: "/mirror",
		Console:    "ttyS0",
	}

	_, err := buildKernelArgs(cfg)
	if err == nil {
		t.Fatalf("buildKernelArgs() error = nil, want error")
	}
}

type noopEnv struct{}

func (noopEnv) Cleanup(build.BuildContext) error { return nil }

func TestVirtInstallBuilderRejectsUnknownEnvironment(t *testing.T) {
	t.Parallel()

	builder := &VirtInstallBuilder{}
	ctx := build.BuildContext{}

	_, err := builder.Build(context.Background(), ctx, noopEnv{})
	if err == nil {
		t.Fatalf("Build() error = nil, want error")
	}

	var buildErr *build.BuildError
	if !errors.As(err, &buildErr) {
		t.Fatalf("expected build error, got %T", err)
	}
}

func TestVirtInstallBuilderPropagatesConfigErrors(t *testing.T) {
	t.Parallel()

	builder := &VirtInstallBuilder{}
	env := &LibvirtBuildEnvironment{}
	ctx := build.BuildContext{
		Spec: build.BuildSpecification{
			Profile: build.BuildProfile{
				PreseedEnabled: false,
			},
		},
	}

	_, err := builder.Build(context.Background(), ctx, env)
	if err == nil {
		t.Fatalf("Build() error = nil, want error")
	}

	var buildErr *build.BuildError
	if !errors.As(err, &buildErr) {
		t.Fatalf("expected build error, got %T", err)
	}
}
