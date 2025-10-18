package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var ConfigDir = "/etc/mime/config"
var StorageDir = "/var/mime/"

var configFiles = [...]string{
	filepath.Join(ConfigDir, "networking.json"),
}

func Verify() error {
	for _, file := range configFiles {
		if _, err := os.Stat(file); err != nil {
			return fmt.Errorf("file %s does not exist", file)
		}
	}
	return nil
}

func ClearConfig() error {
	getLogger().Info("clearing configuration files")

	for _, file := range configFiles {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to remove %s: %w", file, err)
		}
	}
	return nil
}
