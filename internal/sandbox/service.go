package sandbox

import (
	"cochaviz/mime/internal/artifacts"
	"context"
)

type SandboxService struct {
}

func NewSandboxService() *SandboxService {
	return &SandboxService{}
}

// SandboxWorker handles the execution, analysis, instrumentation
// of a sandboxed sample. Only a single SandboxWorker can access a
// particular sandbox.
type SandboxWorker struct {
	sample *Sample

	lease  *SandboxLease
	driver *SandboxDriver

	artifactStore *artifacts.ArtifactStore
}

// NewSandboxWorker creates a new SandboxWorker instance.
func NewSandboxWorker() *SandboxWorker {
	return &SandboxWorker{}
}

// Run executes the sandboxed sample.
func (w *SandboxWorker) Run(ctx context.Context) error {

	return nil
}

// LeaseService provides methods for managing leases.
type LeaseService interface {
	// Add fields here
	Acquire() (SandboxLease, error)
}

// LeaseResolver resolves LeaseRequests to concrete LeaseSpecification objects.
type LeaseResolver interface {
	// Add fields here
}
