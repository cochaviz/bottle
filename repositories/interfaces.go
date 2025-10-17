package repositories

import (
	models "cochaviz/mime/models"
)

// SpecficiationRepository manages sandbox specifications.
type SandboxSpecficiationRepository interface {
	Get(specID string) (models.SandboxSpecification, error)
	Save(spec models.SandboxSpecification) (models.SandboxSpecification, error)

	ListVersions(specID string) ([]models.SandboxSpecification, error)
	ListAll() ([]models.SandboxSpecification, error)

	FilterByArchitecture(architecture string) ([]models.SandboxSpecification, error)
}

// ImageRepository persists metadata for built sandbox images.
type ImageRepository interface {
	Save(image models.SandboxImage) error
	LatestForSpec(specID string) (*models.SandboxImage, error)
	Get(imageID string) (*models.SandboxImage, error)
}

// ArtifactStore stores various artifacts.
type ArtifactStore interface {
	StoreArtifact(artifactPath string, kind models.ArtifactKind, metadata map[string]any) (models.Artifact, error)
	RemoveArtifact(artifact models.Artifact) error
	Clear() error
}

// LeaseRepository exposes CRUD operations for sandbox leases.
type LeaseRepository interface {
	Save(lease models.Lease) (models.Lease, error)
	Get(leaseID string) (*models.Lease, error)
	ListActive() ([]models.Lease, error)
	Delete(leaseID string) error
}

// LeaseLockRepository provides advisory locks for lease operations.
type LeaseLockRepository interface {
	Acquire(leaseID string, owner string, ttlSeconds int) (bool, error)
	Release(leaseID string, owner string) error
}
