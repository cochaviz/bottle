package simple

import (
	specifications "github.com/cochaviz/bottle/internal/build/repositories"
	"os"
	"testing"
)

func TestSimpleBuild(t *testing.T) {
	specRepo := specifications.NewEmbeddedSpecificationRepository()
	arch := "amd64"

	specs, err := specRepo.FilterByArchitecture(arch)

	if err != nil {
		t.Errorf("Error filtering specifications: %v", err)
		return
	}
	if len(specs) == 0 {
		t.Errorf("No specifications found for architecture %s", arch)
		return
	}
	tempImageDir := os.TempDir()
	tempArtifactDir := os.TempDir()

	BuildSandbox(
		specs[0].ID,
		tempImageDir,
		tempArtifactDir,
		"qemu:///session", // Use session mode for testing
		nil,
	)
}
