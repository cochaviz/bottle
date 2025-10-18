package sandbox

import (
	models "cochaviz/mime/internal/models"
)

// SandboxLeaseSpec bridges the driver API with domain models.
type (
	SandboxLeaseSpec = models.LeaseSpecification
	SandboxLease     = models.Lease
)

// SnapshotHandle identifies a driver-created snapshot and any additional metadata
// required to restore or manage it.
type SnapshotHandle struct {
	ID       string
	Metadata map[string]any
}

// LeaseMetrics captures runtime telemetry reported by the sandbox driver.
type LeaseMetrics struct {
	CPUPercent        float64
	MemoryBytes       uint64
	GuestHeartbeatOK  bool
	AdditionalMetrics map[string]any
}

// SandboxDriver describes the contract sandbox adapters must satisfy. The
// methods intentionally mirror the driver boundary proposed in
// internal/design/libvirt.md so orchestration code stays independent from
// hypervisor-specific details.
type SandboxDriver interface {
	Acquire(spec SandboxLeaseSpec) (SandboxLease, error)
	Attach(lease SandboxLease) (SandboxLease, error)
	Pause(lease SandboxLease, reason string) (SandboxLease, error)
	Resume(lease SandboxLease) (SandboxLease, error)
	Snapshot(lease SandboxLease, label string) (SnapshotHandle, error)
	Destroy(lease SandboxLease, force bool) error
	CollectMetrics(lease SandboxLease) (LeaseMetrics, error)
}
