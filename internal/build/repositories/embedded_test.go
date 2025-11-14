package repositories

import (
	"testing"
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

	archs := [5]string{"amd64", "arm64", "x86_64", "i386", "armhf"}

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
