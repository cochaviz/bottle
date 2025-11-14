# Sandbox Execution Pipeline Design

## 1. Objectives
- Provide a deterministic pipeline for executing sandboxes so cycle orchestration, instrumentation, and analysis share a common state machine.
- Let run plans express architecture and image requirements discovered during static analysis so that lease acquisition happens afterwards and remains swappable when runs fail.
- Ensure forwarding rules, capture tooling (tcpdump, Zeek), and guest runners initialize in the right order and can be retried safely.
- Preserve all artifacts and telemetry needed for iterative analysis without tearing down contextual data prematurely.

## 2. Core Concepts
| Model | Purpose |
|-------|---------|
| `SandboxPlan` | Lease-agnostic blueprint produced from sample analysis describing desired image spec, network policy, and instrumentation requests.
| `ForwardingRulePlan` | Declarative NAT/ACL description (host ports, guest networks, inspection taps) compiled into iptables/nftables commands.
| `InstrumentationSession` | Tracks auxiliary services started per run (tcpdump, Zeek, sysmon collectors) including PIDs, sockets, and artifact sinks.
| `SandboxRun` | Lifecycle record emitted by orchestrator (`pending → preparing → executing → analyzing → completed/failed`).
| `RunArtifact` | Catalog entry for pcaps, Zeek logs, guest console output, memory dumps, etc., versioned per run for replay.
| `RunAnalysis` | Result of post-run processing (observable set, scoring, classification, rerun recommendation).
| `LeaseRequest` | Requirements package derived from the sandbox plan (architecture, hypervisor features, preferred images) used to negotiate a concrete lease.

## 3. Lifecycle Overview
1. **Static Analysis & Blueprinting** – Inspect sample, infer architecture and environmental constraints, and emit a `SandboxPlan` plus `LeaseRequest` that captures required capabilities without binding to a specific VM.
2. **Lease Negotiation** – Pass the request to `SandboxLeaseManager`; acquire (or reuse) a lease that satisfies the run plan. If no lease matches, surface the deficiency so the planner can modify the blueprint.
3. **Plan Binding** – Combine the lease with the run blueprint, materializing concrete execution parameters (guest identifiers, paths) while ensuring plan overrides take precedence when conflicts arise.
4. **Forwarding Prep** – Compile `ForwardingRulePlan`, stage iptables/nftables commands, provision capture bridges, validate no conflicts.
5. **Instrumentation Start** – Launch tcpdump, optional Zeek, and host-side logging with coordinated time sync and health checks.
6. **Sandbox Execution** – Attach to the bound lease, push sample payload, drive guest automation, and emit execution telemetry.
7. **Teardown & Harvest** – Stop instrumentation, flush buffers, compute checksums, and register `RunArtifact` entries.
8. **Analysis Loop** – Run parsers/analytics, persist `RunAnalysis`, and decide whether the scheduler should enqueue another iteration (including re-blueprinting when architecture changes are suggested).

## 4. Plan Resolution & Dependencies
- `SandboxPlanner` first generates a lease-agnostic blueprint by interrogating static analysis output, catalog metadata (image capabilities, supported architectures), and policy services.
- The planner derives a `LeaseRequest` that encodes minimum viable parameters (arch, virtualization features, guest tooling) and preferred image identifiers exposed by repositories.
- `SandboxLeaseManager` uses the request to acquire or attach to a lease; if the returned lease differs from the preferred spec, a reconciliation step verifies compatibility or instructs the planner to re-emit a modified request.
- Resolve profile-driven defaults (timeout, guest automation scripts, instrumentation presets) before any side effects so the plan stays consistent even if multiple lease attempts occur.
- Persist both the blueprint and the bound plan so retries can restore intended configuration, adjust architecture, or swap images deterministically.

## 5. Forwarding Rule Orchestration
- Maintain a pluggable `ForwardingRuleCompiler` that transforms `ForwardingRulePlan` into host-specific commands (iptables, nftables, pf).
- Support rule templates: inbound proxying, egress gating, DNS sinkhole routing, and analyst interactive tunnels.
- Pre-flight checks: ensure no port conflicts, verify kernel modules, and dry-run commands with `--check` where supported.
- Apply rules with idempotent transactions, capturing rollback scripts to revert on failure or at teardown.
- Emit structured events for rule application/removal for audit trails and troubleshooting.

## 6. Instrumentation & Auxiliary Services
- `InstrumentationController` spawns registered plugins (`tcpdump`, Zeek, process monitors) based on plan presets.
- Each plugin implements hooks: `prepare(env)`, `start(run_id)`, `stop()`, `collect_artifacts(destination)`.
- Provide coordinated clock sync by writing a run-specific marker file and recording host monotonic timestamps.
- Allow sampling policies (full capture vs. selective filters) and attach metadata (capture filter, interface, rotation policy) to artifacts.
- Health monitor polls processes, ensures disk space, and triggers graceful shutdown if thresholds exceeded.

## 7. Sandbox Execution Engine
- `SandboxRunnerService` drives the state machine, integrating with adapters (e.g., `LibvirtSandboxDriver`).
- Sequence:
  - Bind the lease returned by `SandboxLeaseManager` to the `SandboxPlan`, giving precedence to plan-declared overrides (e.g., custom kernel arguments) while validating compatibility.
  - Verify guest readiness (agent heartbeat, network availability) before transferring payloads so quick lease swaps remain safe.
  - Transfer sample payload and automation scripts via `BuildArtifactRepository` or guest agent.
  - Kick off execution (e.g., run command, schedule tasks) while streaming stdout/stderr to telemetry bus.
  - Monitor for completion, timeouts, or policy violations; issue pause/destroy if run exceeds constraints; if execution fails due to architecture mismatch, feed the observation back to the planner for blueprint revision.
- Emit execution snapshots for observability and debugging parity with other services.

## 8. Teardown, Artifact Capture, and Cataloging
- Stop instrumentation before removing forwarding rules to avoid packet loss; flush buffers to disk/object storage.
- Compress and checksum artifacts; register them through `ArtifactRepository` with references to `RunArtifact` records.
- Collect guest-side logs (Windows Event Logs, syslog, file drops) via defined adapters and store alongside network captures.
- Capture rule summaries and instrumentation metadata (versions, command lines) to aid reproducibility.

## 9. Analysis Loop & Iteration Strategy
- `RunAnalysisService` consumes artifacts asynchronously, executes parsers (Zeek intelligence, YARA, sandbox heuristics), and emits `RunAnalysis` objects.
- Decision engine examines analysis outputs and policy rules to instruct scheduler:
  - Repeat run with modified forwarding rules (e.g., open outbound port) or additional instrumentation.
  - Reissue a blueprint with alternative architecture or image candidates when the sample fails under the current lease.
  - Escalate to manual review if anomalies detected (e.g., undesired beaconing).
  - Promote observables to allow-list or block-list services.
- Maintain lineage so successive runs reference previous artifacts, lease attempts, and changes to forwarding plans.

## 10. Observability & Reliability
- Structured logging with run IDs, instrumentation IDs, and forwarding rule hashes for traceability.
- Metrics: rule application latency, instrumentation uptime, run duration, artifact size, analysis turnaround.
- Expose health endpoints for instrumentation plugins and forwarding rule engines.
- Provide crash recovery: detect orphaned rules or lingering processes on startup and reconcile against persisted plans.

## 11. Implementation Roadmap
1. Introduce lease-agnostic planning models (`SandboxPlan`, `LeaseRequest`) alongside existing telemetry artefacts.
2. Build planner workflow that consumes static analysis output, emits blueprints, and negotiates leases through `SandboxLeaseManager`.
3. Extend `ForwardingRuleCompiler` and instrumentation controller to operate on the bound plan while remaining compatible with blueprint retries.
4. Wrap existing runner scripts in `SandboxRunnerService`, ensuring plan overrides win when binding to leases.
5. Create artifact cataloging + analysis services; wire scheduler feedback loop for reruns and re-blueprinting.
6. Add observability surface (metrics/logs) and recovery routines for orphaned resources across multiple lease attempts.
