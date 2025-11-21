package simple

import (
	"os"
	"testing"

	"github.com/cochaviz/bottle/arch"
	specifications "github.com/cochaviz/bottle/internal/build/repositories"
)

func TestSimpleBuild(t *testing.T) {
	specRepo := specifications.NewEmbeddedSpecificationRepository()
	targetArch := arch.X86_64

	specs, err := specRepo.FilterByArchitecture(targetArch)

	if err != nil {
		t.Errorf("Error filtering specifications: %v", err)
		return
	}
	if len(specs) == 0 {
		t.Errorf("No specifications found for architecture %s", targetArch)
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
