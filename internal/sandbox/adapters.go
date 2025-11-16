package sandbox

import "fmt"

// SnapshotHandle identifies a driver-created snapshot and any additional metadata
// required to restore or manage it.
type SnapshotHandle struct {
	ID       string
	Metadata map[string]any
}

// SandboxMetrics captures runtime telemetry reported by the sandbox driver.
type SandboxMetrics struct {
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
	Acquire(spec SandboxLeaseSpecification) (SandboxLease, error)
	Start(lease SandboxLease) (SandboxLease, error)
	// Attach(lease SandboxLease) (SandboxLease, error) // we don't run interactively so we don't need this
	Pause(lease SandboxLease, reason string) (SandboxLease, error)
	Resume(lease SandboxLease) (SandboxLease, error)
	// Snapshot(lease SandboxLease, label string) (SnapshotHandle, error) // currently, we stream network data so we don't support snapshots now
	Release(lease SandboxLease, force bool) error
	CollectMetrics(lease SandboxLease) (SandboxMetrics, error)
}

type SandboxInterruptedError struct {
	Reason string
}

func (e SandboxInterruptedError) Error() string {
	return fmt.Sprintf("sandbox interrupted: %s", e.Reason)
}
