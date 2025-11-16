package repositories

import (
	_ "embed"
	"fmt"
	"os"
	"sync"
)

//go:embed assets/preseed.cfg
var embeddedPreseed string

//go:embed assets/build-network.xml
var embeddedBuildNetwork string

//go:embed assets/bringup-dhcp.sh
var embeddedBringupDHCP string

var (
	setupScriptOnce sync.Once
	setupScriptPath string
	setupScriptErr  error
)

func materializeSetupScript() (string, error) {
	setupScriptOnce.Do(func() {
		f, err := os.CreateTemp("", "mime-bringup-dhcp-*.sh")
		if err != nil {
			setupScriptErr = fmt.Errorf("create temp setup script: %w", err)
			return
		}

		cleanup := func(e error) {
			setupScriptErr = e
			f.Close()
			_ = os.Remove(f.Name())
		}

		if _, err := f.WriteString(embeddedBringupDHCP); err != nil {
			cleanup(fmt.Errorf("write setup script: %w", err))
			return
		}
		if err := f.Chmod(0o755); err != nil {
			cleanup(fmt.Errorf("chmod setup script: %w", err))
			return
		}
		if err := f.Close(); err != nil {
			cleanup(fmt.Errorf("close setup script: %w", err))
			return
		}
		setupScriptPath = f.Name()
	})
	return setupScriptPath, setupScriptErr
}
