package sandbox

type SandboxSpecficationRepository interface {
	Get(specID string) (SandboxSpecification, error)
	Save(spec SandboxSpecification) (SandboxSpecification, error)

	ListVersions(specID string) ([]SandboxSpecification, error)
	ListAll() ([]SandboxSpecification, error)

	FilterByArchitecture(architecture string) ([]SandboxSpecification, error)
}

// ImageRepository persists metadata for built sandbox images.
type ImageRepository interface {
	Save(image SandboxImage) error
	LatestForSpec(specID string) (*SandboxImage, error)
	Get(imageID string) (*SandboxImage, error)
}

// LeaseRepository exposes CRUD operations for sandbox leases.
type LeaseRepository interface {
	Save(lease SandboxLease) (SandboxLease, error)
	Get(leaseID string) (*SandboxLease, error)
	ListActive() ([]SandboxLease, error)
	Delete(leaseID string) error
}

// LeaseLockRepository provides advisory locks for lease operations.
type LeaseLockRepository interface {
	Acquire(leaseID string, owner string, ttlSeconds int) (bool, error)
	Release(leaseID string, owner string) error
}
