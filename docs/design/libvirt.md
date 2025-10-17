# Libvirt Integration Design

## 1. Purpose
Isolate libvirt-specific calls behind a driver boundary so the orchestration layer (`SandboxLeaseManager`, `SandboxCycleService`) works purely with domain records (`SandboxLease`, `SandboxCycle`) and remains restart-friendly.​:codex-file-citation[codex-file-citation]{line_range_start=8 line_range_end=58 path=docs/long_running_sandbox.md git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/docs/long_running_sandbox.md#L8-L58"}​

## 2. Abstractions
- **`SandboxDriver` interface**
  - `acquire(spec: SandboxLeaseSpec) -> SandboxLeaseHandle`
  - `attach(lease: SandboxLease) -> SandboxLease`
  - `pause(lease: SandboxLease, reason: str) -> SandboxLease`
  - `resume(lease: SandboxLease) -> SandboxLease`
  - `snapshot(lease: SandboxLease, label: str) -> SnapshotHandle`
  - `destroy(lease: SandboxLease, force: bool = False) -> None`
  - `collect_metrics(lease: SandboxLease) -> LeaseMetrics`
- **`LibvirtSandboxDriver` implementation**
  - Wraps libvirt connection pooling, domain lookup, network management, and error translation (e.g., map `libvirtError` to domain exceptions).
  - Stores serialized handle `{driver: "libvirt", uri, domain_uuid}` inside `SandboxLease` metadata for rehydration after process restarts.

## 3. Supporting Structures
- `SandboxLeaseSpec`: desired run profile, network name, RAM/vCPU hints, allow-list snapshot reference.
- `SandboxLeaseHandle`: internal data (domain UUID, connection URI, active network names).
- `LeaseMetrics`: CPU usage, memory balloon, agent heartbeat status, guest tools state.

## 4. Lifecycle Sequences
1. **Acquire**
   - Lease manager requests lease.
   - Driver opens connection (`libvirt.open(uri)`), ensures network exists, clones overlay, defines domain if missing, and returns `SandboxLease` with `state="ready"`.
2. **Attach**
   - On cycle start, driver reopens connection, performs `lookupByUUIDString`, refreshes domain info, and returns updated lease with heartbeat timestamp.
3. **Pause / Resume for Allow-List Updates**
   - `pause`: call `domain.suspend()`, wait for completion, snapshot guest state (optional), signal allow-list deployment tasks.
   - After allow-list revision applied, `resume`: call `domain.resume()`, update `SandboxLease.state="executing"` and record iteration metadata.
4. **Snapshot / Revert**
   - Provide optional ability to create libvirt snapshots tied to `SessionIteration` to aid debugging.
5. **Destroy**
   - On lease expiration or unrecoverable error, driver issues `destroy` and cleans overlay/network resources.

## 5. Error Handling
- Standardize exception hierarchy (`LeaseUnavailableError`, `DomainLostError`, `PauseTimeoutError`).
- Implement retry/backoff policies for transient libvirt errors.
- Provide `recover(lease)` hook to rebuild domain if the guest crashes mid-cycle.

## 6. Telemetry
- Emit events for pause/resume operations, include libvirt reason codes.
- Collect guest agent responsiveness via `qemuAgentCommand`.
- Integrate with metrics collector (Prometheus exporter) for CPU/memory, domain state transitions.

## 7. Testing Strategy
- Unit-test driver logic with libvirt mocking (using `pytest` + `unittest.mock`).
- Integration tests against a disposable libvirt connection (CI job with qemu-kvm).
- Replay failure scenarios (domain missing, network offline) to validate recovery hooks.

## 8. Migration Notes
- Start by encapsulating current `talkbox.sandbox.run` helper functions inside `LibvirtSandboxDriver`; use adapter inside existing CLI to ensure parity.​:codex-file-citation[codex-file-citation]{line_range_start=1 line_range_end=200 path=talkbox/sandbox/run.py git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/talkbox/sandbox/run.py#L1-L200"}​
- Gradually swap orchestration code to consume the driver through dependency injection.
