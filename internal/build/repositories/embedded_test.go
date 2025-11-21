package repositories

import (
	"testing"

	"github.com/cochaviz/bottle/arch"
)

func TestRepositoryHasEntries(t *testing.T) {
	repo := NewEmbeddedSpecificationRepository()
	specs, err := repo.ListAll()

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if len(specs) == 0 {
		t.Errorf("Expected at least one specification, got %d", len(specs))
	}
}

func TestRepositoryHasCorrectEntries(t *testing.T) {
	repo := NewEmbeddedSpecificationRepository()

	archs := []arch.Architecture{
		arch.X86_64,
		arch.I686,
		arch.AArch64,
		arch.ARMV7L,
		arch.PPC64LE,
		arch.S390X,
		arch.MIPSEL,
	}

	for _, arch := range archs {
		specs, err := repo.FilterByArchitecture(arch)

		if err != nil {
			t.Errorf("Unexpected error for architecture %s: %v", arch, err)
		}

		if len(specs) == 0 {
			t.Errorf("Expected at least one specification for architecture %s, got %d", arch, len(specs))
		}
	}
}
