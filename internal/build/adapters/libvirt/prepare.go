package libvirt

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/cochaviz/bottle/internal/build"

	libvirt "libvirt.org/go/libvirt"
)

// Ensure LibvirtBuildEnvironmentPreparer implements the EnvironmentPreparer interface.
var _ build.BuildEnvironmentPreparer = (*LibvirtBuildEnvironmentPreparer)(nil)

// StoragePoolCleaner abstracts libvirt storage cleanup to simplify testing.
type StoragePoolCleaner interface {
	CleanupStoragePool(connectionURI, targetPath string) error
}

// LibvirtBuildEnvironmentPreparer supplies a temporary workspace for the libvirt builder.
type LibvirtBuildEnvironmentPreparer struct {
	BaseDir            string
	ConnectionURI      string
	StoragePoolCleaner StoragePoolCleaner // Uses LibvirtStoragePoolCleaner by default
}

// Prepare provisions the workspace directory and writes optional provisioning assets.
func (p *LibvirtBuildEnvironmentPreparer) Prepare(ctx build.BuildContext) (build.BuildEnvironment, error) {
	workDir := filepath.Join(p.BaseDir, "build")

	if p.StoragePoolCleaner == nil {
		p.StoragePoolCleaner = &LibvirtStoragePoolCleaner{}
	}

	info, err := os.Stat(p.BaseDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("base dir %q does not exist", p.BaseDir)
		}
		return nil, fmt.Errorf("stat base dir %q: %w", p.BaseDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("base dir %q is not a directory", p.BaseDir)
	}

	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("create workdir: %w", err)
	}

	if err := ensureExecutePermissions(workDir); err != nil {
		return nil, err
	}

	var preseedPathPtr *string
	if content := readInstallerAsset(ctx.Spec.InstallerAssets, "preseed_content"); content != "" {
		path := filepath.Join(workDir, "preseed.cfg")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("write preseed config: %w", err)
		}
		preseedPathPtr = &path
	}

	var networkPathPtr *string
	if content := readInstallerAsset(ctx.Spec.InstallerAssets, "network_configuration"); content != "" {
		path := filepath.Join(workDir, "network.xml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("write network config: %w", err)
		}
		networkPathPtr = &path
	}

	return &LibvirtBuildEnvironment{
		WorkDir:                  workDir,
		PreseedConfigPath:        preseedPathPtr,
		NetworkConfigurationPath: networkPathPtr,
		storagePoolCleaner:       p.StoragePoolCleaner,
		ConnectURI:               p.ConnectionURI,
	}, nil
}

var _ build.BuildEnvironment = (*LibvirtBuildEnvironment)(nil)

type LibvirtBuildEnvironment struct {
	WorkDir                  string
	PreseedConfigPath        *string
	NetworkConfigurationPath *string
	storagePoolCleaner       StoragePoolCleaner
	ConnectURI               string
}

// Cleanup removes the workspace and attempts libvirt storage cleanup.
func (env *LibvirtBuildEnvironment) Cleanup(ctx build.BuildContext) error {
	var cleanupErr error

	if env.WorkDir != "" {
		if err := os.RemoveAll(env.WorkDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove workdir: %w", err))
		}
	}

	if env.storagePoolCleaner != nil && env.ConnectURI != "" && env.WorkDir != "" {
		if err := env.storagePoolCleaner.CleanupStoragePool(env.ConnectURI, env.WorkDir); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup storage pool: %w", err))
		}
	}

	return cleanupErr
}

func readInstallerAsset(assets map[string]string, key string) string {
	if assets == nil {
		return ""
	}

	return assets[key]
}

func ensureExecutePermissions(path string) error {
	for dir := path; ; {
		if info, err := os.Stat(dir); err == nil {
			currentPerm := info.Mode().Perm()
			desiredPerm := currentPerm | 0o755
			if desiredPerm != currentPerm {
				newMode := info.Mode()&^os.ModePerm | desiredPerm
				if err := os.Chmod(dir, newMode); err != nil {
					if errors.Is(err, fs.ErrPermission) {
						break
					}
					return fmt.Errorf("chmod %q: %w", dir, err)
				}
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("stat %q: %w", dir, err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return nil
}

// LibvirtStoragePoolCleaner cleans up a libvirt storage pool.
type LibvirtStoragePoolCleaner struct{}

func (LibvirtStoragePoolCleaner) CleanupStoragePool(connectionURI, targetPath string) error {
	conn, err := libvirt.NewConnect(connectionURI)
	if err != nil {
		return err
	}
	defer conn.Close()

	pool, err := conn.LookupStoragePoolByTargetPath(targetPath)
	if err != nil {
		if isInLibvirtErrors(err, libvirt.ERR_NO_STORAGE_POOL) {
			return nil
		}
		return err
	}
	defer pool.Free()

	active, err := pool.IsActive()
	if err == nil && active {
		if err := pool.Destroy(); err != nil {
			if !isInLibvirtErrors(err, libvirt.ERR_OPERATION_INVALID, libvirt.ERR_NO_STORAGE_POOL) {
				return err
			}
		}
	}

	if err := pool.Undefine(); err != nil {
		if isInLibvirtErrors(err, libvirt.ERR_NO_STORAGE_POOL) {
			return nil
		}
		return err
	}

	return nil
}

func isInLibvirtErrors(err error, codes ...libvirt.ErrorNumber) bool {
	if err == nil {
		return false
	}

	// try convert error to libvirt error
	var libErr libvirt.Error
	if !errors.As(err, &libErr) {
		return false
	}

	return slices.Contains(codes, libErr.Code)
}
