package sandbox

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kdomanski/iso9660"
)

// prepareSandboxDisk creates a read-only disk image from the provided directory.
// The directory is mirrored into the run directory to avoid mutating the source.
func prepareSandboxDisk(runDir, sourceDir, diskName, volumeLabel string, markSetup bool) (string, error) {
	srcAbs, err := filepath.Abs(sourceDir)
	if err != nil {
		return "", fmt.Errorf("resolve %s directory %q: %w", diskName, sourceDir, err)
	}
	info, err := os.Stat(srcAbs)
	if err != nil {
		return "", fmt.Errorf("stat %s directory %q: %w", diskName, srcAbs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s path %q is not a directory", diskName, srcAbs)
	}

	stagingDir := filepath.Join(runDir, diskName+"_data")
	if err := os.RemoveAll(stagingDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("clear %s staging directory: %w", diskName, err)
	}
	if err := copyDirectoryContents(srcAbs, stagingDir); err != nil {
		return "", fmt.Errorf("copy %s directory: %w", diskName, err)
	}
	if markSetup {
		markerPath := filepath.Join(stagingDir, "setup")
		if err := os.WriteFile(markerPath, nil, 0o644); err != nil {
			return "", fmt.Errorf("create setup marker: %w", err)
		}
	}

	imagePath := filepath.Join(runDir, diskName+".iso")
	if err := createISOFromDirectory(stagingDir, imagePath, volumeLabel); err != nil {
		return "", fmt.Errorf("create %s disk image: %w", diskName, err)
	}
	return imagePath, nil
}

func copyDirectoryContents(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dstDir, rel)

		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()

		if mode&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported in sandbox disks (%s)", path)
		}

		if d.IsDir() {
			if rel == "." {
				return os.MkdirAll(dstDir, mode.Perm())
			}
			return os.MkdirAll(targetPath, mode.Perm())
		}

		if !mode.IsRegular() {
			return fmt.Errorf("unsupported file type %s in %s", mode, path)
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		return copyFile(path, targetPath, mode.Perm())
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil
}

func createISOFromDirectory(sourceDir, imagePath, volumeLabel string) error {
	writer, err := iso9660.NewWriter()
	if err != nil {
		return fmt.Errorf("create iso writer: %w", err)
	}
	defer writer.Cleanup()

	if err := writer.AddLocalDirectory(sourceDir, "/"); err != nil {
		return fmt.Errorf("stage directory: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(imagePath), 0o755); err != nil {
		return fmt.Errorf("ensure image directory: %w", err)
	}

	out, err := os.OpenFile(imagePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create image file: %w", err)
	}

	if err := writer.WriteTo(out, volumeLabel); err != nil {
		_ = os.Remove(imagePath)
		return fmt.Errorf("write iso: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(imagePath)
		return fmt.Errorf("finalize iso: %w", err)
	}
	return nil
}

func sanitizeVolumeLabel(parts ...string) string {
	const maxLen = 32

	label := strings.Join(parts, "_")
	if label == "" {
		label = "SANDBOX"
	}

	var b strings.Builder
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
		if b.Len() >= maxLen {
			break
		}
	}

	result := b.String()
	if result == "" {
		return "SANDBOX"
	}
	return result
}

func resolveDeviceLetter(preferred, fallback string) string {
	letter := strings.TrimSpace(strings.ToLower(preferred))
	if letter != "" {
		return letter
	}
	return strings.TrimSpace(strings.ToLower(fallback))
}

func nextDeviceLetter(letter string) string {
	letter = strings.TrimSpace(strings.ToLower(letter))
	if len(letter) != 1 || letter[0] < 'a' || letter[0] > 'y' {
		return "c"
	}
	return string(letter[0] + 1)
}

func cdDeviceTarget(prefix, letter string) string {
	devicePrefix := strings.TrimSpace(prefix)
	if devicePrefix == "" {
		devicePrefix = "sd"
	}
	letter = strings.TrimSpace(strings.ToLower(letter))
	if letter == "" {
		letter = "b"
	}
	return devicePrefix + letter
}
