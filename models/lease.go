package models

import (
	"time"
)

type LeaseState = string

type LeaseRequest struct {
	ID           string
	Architecture string

	constraints map[string]any
	metadata    map[string]any
}

type LeaseSpecification struct {
	SandboxSpecification SandboxSpecification
	SandboxImage         SandboxImage

	ttl time.Duration // Duration for which the lease is valid.
}

type Lease struct {
	ID        int64
	StartTime time.Time
	EndTime   time.Time

	Specification LeaseSpecification
	State         LeaseState

	RuntimeConfig map[string]any
	Metadata      map[string]any

	// connect_uri: str
	// vm_name: str
	// vm_ip: str
	// base_image: Path
	// overlay_path: Path
	// work_dir: Path
	// network_name: str | None = None
	// vm_uuid: str | None = None
	// run_profile: RunProfile | None = None
	// domain_profile: DomainProfile | None = None
	// health: dict[str, object] = field(default_factory=dict)
	// metadata: dict[str, object] = field(default_factory=dict)
	// domain_xml: str = ""
}
