package specifications

import (
	"cochaviz/mime/internal/sandbox"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestLocalSpecificationRepositorySaveAndGet(t *testing.T) {
	repo := newTestSpecificationRepository(t)
	spec := newTestSpecification(t)

	want, err := repo.Save(*spec)

	if err != nil {
		t.Fatalf("failed to save specification: %v", err)
	}

	got, err := repo.Get(want.ID)
	if err != nil {
		t.Fatalf("failed to get specification: %v", err)
	}
	equalsSpecification(t, &got, &want)
}

func equalsSpecification(t *testing.T, got *sandbox.SandboxSpecification, want *sandbox.SandboxSpecification) {
	if got == nil && want == nil {
		return
	}
	if got == nil || want == nil {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got.ID != want.ID {
		t.Errorf("got ID %s, want %s", got.ID, want.ID)
	}
	if got.Version != want.Version {
		t.Errorf("got Version %s, want %s", got.Version, want.Version)
	}
	if got.OSRelease != want.OSRelease {
		t.Errorf("got OSRelease %s, want %s", got.OSRelease, want.OSRelease)
	}
	if !reflect.DeepEqual(got.DomainProfile, want.DomainProfile) {
		t.Errorf("got DomainProfile %v, want %v", got.DomainProfile, want.DomainProfile)
	}
	if !reflect.DeepEqual(got.RunProfile, want.RunProfile) {
		t.Errorf("got RunProfile %v, want %v", got.RunProfile, want.RunProfile)
	}
}

func newTestSpecificationRepository(t *testing.T) *LocalSandboxSpecificationRepository {
	return &LocalSandboxSpecificationRepository{
		BaseDir: t.TempDir(),
	}
}

func newTestSpecification(t *testing.T) *sandbox.SandboxSpecification {
	return &sandbox.SandboxSpecification{
		ID:        uuid.New().String(),
		Version:   "1.0",
		OSRelease: "test",

		DomainProfile: sandbox.DomainProfile{},
		RunProfile:    sandbox.RunProfile{},
	}
}
