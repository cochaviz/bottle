package simple

import (
	"cochaviz/mime/internal/repositories/embedded"
	"os"
	"testing"
)

func TestSimpleBuild(t *testing.T) {
	specRepo := embedded.NewEmbeddedSpecificationRepository()

	specs, err := specRepo.FilterByArchitecture("x86_64")
	if err != nil {
		t.Errorf("Error filtering specifications: %v", err)
	}
	if len(specs) == 0 {
		t.Errorf("No specifications found for architecture 'amd64'")
	}
	tempImageDir := os.TempDir()
	tempArtifactDir := os.TempDir()

	Build(
		specs[0].ID,
		tempImageDir,
		tempArtifactDir,
		"qemu:///session", // Use session mode for testing
	)
}
