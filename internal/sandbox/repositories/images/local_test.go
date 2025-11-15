package images

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"cochaviz/mime/internal/artifacts"
	"cochaviz/mime/internal/sandbox"
)

func TestLocalImageRepositorySaveAndGet(t *testing.T) {
	t.Parallel()

	repo := LocalImageRepository{BaseDir: t.TempDir()}
	want := newTestImage("image-123", "spec-1", time.Unix(1_700_000_000, 0))

	if err := repo.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := repo.Get(want.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	assertSandboxImageEqual(t, want, got)
}

func TestLocalImageRepositoryLatestForSpec(t *testing.T) {
	t.Parallel()

	repo := LocalImageRepository{BaseDir: t.TempDir()}

	images := []sandbox.SandboxImage{
		newTestImage("img-old", "spec-a", time.Unix(1_700_000_000, 0)),
		newTestImage("img-new", "spec-a", time.Unix(1_800_000_000, 0)),
		newTestImage("img-other", "spec-b", time.Unix(1_750_000_000, 0)),
	}

	for _, img := range images {
		if err := repo.Save(img); err != nil {
			t.Fatalf("Save(%q) error = %v", img.ID, err)
		}
	}

	got, err := repo.LatestForSpec("spec-a")
	if err != nil {
		t.Fatalf("LatestForSpec() error = %v", err)
	}

	assertSandboxImageEqual(t, images[1], got)
}

func TestLocalImageRepositoryLatestForSpecMissingDir(t *testing.T) {
	t.Parallel()

	repo := LocalImageRepository{
		BaseDir: filepath.Join(t.TempDir(), "missing"),
	}

	got, err := repo.LatestForSpec("spec-a")
	if err != nil {
		t.Fatalf("LatestForSpec() error = %v", err)
	}
	if got != nil {
		t.Fatalf("LatestForSpec() = %+v, want nil", got)
	}
}

func newTestImage(id, specID string, createdAt time.Time) sandbox.SandboxImage {
	return sandbox.SandboxImage{
		ID: id,
		ReferenceSpecification: sandbox.SandboxSpecification{
			ID:      specID,
			Version: "v1",
		},
		ImageArtifact: artifacts.Artifact{
			ID:   "artifact-" + id,
			Kind: artifacts.ImageArtifact,
			URI:  "file:///tmp/" + id + ".qcow2",
		},
		CreatedAt: createdAt.UTC(),
		Metadata: map[string]any{
			"source": id,
		},
		CompanionArtifacts: []artifacts.Artifact{
			{
				ID:   "companion-" + id,
				Kind: artifacts.BuildArtifact,
				URI:  "file:///tmp/" + id + ".log",
			},
		},
	}
}

func assertSandboxImageEqual(t *testing.T, want sandbox.SandboxImage, got *sandbox.SandboxImage) {
	t.Helper()

	if got == nil {
		t.Fatalf("sandbox image = nil, want %+v", want)
	}
	if got.ID != want.ID {
		t.Fatalf("ID = %q, want %q", got.ID, want.ID)
	}
	if !reflect.DeepEqual(got.ReferenceSpecification, want.ReferenceSpecification) {
		t.Fatalf("Specification = %+v, want %+v", got.ReferenceSpecification, want.ReferenceSpecification)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	if !reflect.DeepEqual(got.ImageArtifact, want.ImageArtifact) {
		t.Fatalf("Image = %+v, want %+v", got.ImageArtifact, want.ImageArtifact)
	}
	if !reflect.DeepEqual(got.Metadata, want.Metadata) {
		t.Fatalf("Metadata = %+v, want %+v", got.Metadata, want.Metadata)
	}
	if !reflect.DeepEqual(got.CompanionArtifacts, want.CompanionArtifacts) {
		t.Fatalf("CompanionArtifacts = %+v, want %+v", got.CompanionArtifacts, want.CompanionArtifacts)
	}
}
