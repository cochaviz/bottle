package libvirt

import (
	"github.com/cochaviz/bottle/internal/build"
	"github.com/cochaviz/bottle/internal/sandbox"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

type stubCleaner struct {
	calls int
	uri   string
	path  string
	err   error
}

func (s *stubCleaner) CleanupStoragePool(uri, path string) error {
	s.calls++
	s.uri = uri
	s.path = path
	return s.err
}

func TestPrepareCreatesWorkspaceAndAssets(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	preparer := &LibvirtBuildEnvironmentPreparer{
		BaseDir:            baseDir,
		StoragePoolCleaner: &stubCleaner{},
		ConnectionURI:      "qemu:///system",
	}

	ctx := build.BuildContext{
		Spec: build.BuildSpecification{
			SandboxSpecification: sandbox.SandboxSpecification{},
			InstallerAssets: map[string]string{
				"preseed_content":       "d-i preseed/late_command string",
				"network_configuration": "<network/>",
			},
		},
	}

	env, err := preparer.Prepare(ctx)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	libvirtEnv, ok := env.(*LibvirtBuildEnvironment)
	if !ok {
		t.Fatalf("environment is %T, want *LibvirtBuildEnvironment", env)
	}

	t.Cleanup(func() {
		if err := env.Cleanup(ctx); err != nil {
			t.Errorf("Cleanup() error = %v", err)
		}
	})

	if libvirtEnv.WorkDir == "" {
		t.Fatalf("workdir is empty")
	}

	if libvirtEnv.PreseedConfigPath == nil {
		t.Fatalf("PreseedConfigPath is nil")
	}
	if content, err := os.ReadFile(*libvirtEnv.PreseedConfigPath); err != nil {
		t.Fatalf("read preseed: %v", err)
	} else if got := string(content); got != ctx.Spec.InstallerAssets["preseed_content"] {
		t.Fatalf("preseed content = %q, want %q", got, ctx.Spec.InstallerAssets["preseed_content"])
	}

	if libvirtEnv.NetworkConfigurationPath == nil {
		t.Fatalf("NetworkConfigurationPath is nil")
	}
	if content, err := os.ReadFile(*libvirtEnv.NetworkConfigurationPath); err != nil {
		t.Fatalf("read network config: %v", err)
	} else if got := string(content); got != ctx.Spec.InstallerAssets["network_configuration"] {
		t.Fatalf("network config = %q, want %q", got, ctx.Spec.InstallerAssets["network_configuration"])
	}
}

func TestCleanupRemovesWorkspaceAndInvokesCleaner(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	cleaner := &stubCleaner{}
	preparer := &LibvirtBuildEnvironmentPreparer{
		BaseDir:            baseDir,
		StoragePoolCleaner: cleaner,
		ConnectionURI:      "qemu:///system",
	}

	ctx := build.BuildContext{
		Spec: build.BuildSpecification{
			SandboxSpecification: sandbox.SandboxSpecification{},
		},
	}

	env, err := preparer.Prepare(ctx)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	libvirtEnv := env.(*LibvirtBuildEnvironment)

	if err := env.Cleanup(ctx); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	if cleaner.calls != 1 {
		t.Fatalf("CleanupStoragePool calls = %d, want 1", cleaner.calls)
	}

	if cleaner.uri != libvirtEnv.ConnectURI {
		t.Fatalf("CleanupStoragePool uri = %q, want %q", cleaner.uri, libvirtEnv.ConnectURI)
	}

	if cleaner.path != libvirtEnv.WorkDir {
		t.Fatalf("CleanupStoragePool path = %q, want %q", cleaner.path, libvirtEnv.WorkDir)
	}

	if _, err := os.Stat(libvirtEnv.WorkDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("workdir exists after cleanup: err=%v", err)
	}
}

func TestPrepareWithoutAssets(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	preparer := &LibvirtBuildEnvironmentPreparer{
		BaseDir:            baseDir,
		StoragePoolCleaner: &stubCleaner{},
		ConnectionURI:      "qemu:///system",
	}

	ctx := build.BuildContext{
		Spec: build.BuildSpecification{
			SandboxSpecification: sandbox.SandboxSpecification{},
			InstallerAssets:      map[string]string{},
		},
	}

	env, err := preparer.Prepare(ctx)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	libvirtEnv, ok := env.(*LibvirtBuildEnvironment)
	if !ok {
		t.Fatalf("environment is %T, want *LibvirtBuildEnvironment", env)
	}

	t.Cleanup(func() {
		if err := env.Cleanup(ctx); err != nil {
			t.Errorf("Cleanup() error = %v", err)
		}
	})

	if libvirtEnv.PreseedConfigPath != nil {
		t.Fatalf("PreseedConfigPath = %v, want nil", *libvirtEnv.PreseedConfigPath)
	}

	if libvirtEnv.NetworkConfigurationPath != nil {
		t.Fatalf("NetworkConfigurationPath = %v, want nil", *libvirtEnv.NetworkConfigurationPath)
	}

	preseedPath := filepath.Join(libvirtEnv.WorkDir, "preseed.cfg")
	if _, err := os.Stat(preseedPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("preseed file %q exists unexpectedly", preseedPath)
	}

	networkPath := filepath.Join(libvirtEnv.WorkDir, "network.xml")
	if _, err := os.Stat(networkPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("network file %q exists unexpectedly", networkPath)
	}
}
