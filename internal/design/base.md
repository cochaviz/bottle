# Talkbox Long-Running Sandbox Architecture

## 1. Objectives
- Keep sandbox VMs alive across cycles so that log parsing and allow-list promotion happen between executions instead of after teardown, preserving iteration context for long-lived samples.​:codex-file-citation[codex-file-citation]{line_range_start=3 line_range_end=36 path=docs/long_running_sandbox.md git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/docs/long_running_sandbox.md#L3-L36"}​
- Provide deterministic state transitions for orchestration so REST clients, CLI tooling, and background schedulers observe consistent lifecycle events.​:codex-file-citation[codex-file-citation]{line_range_start=8 line_range_end=58 path=docs/long_running_sandbox.md git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/docs/long_running_sandbox.md#L8-L58"}​
- Decouple artifact capture, post-processing, and allow-list updates into their own services to ease future expansion (e.g., additional parsers or export targets).​:codex-file-citation[codex-file-citation]{line_range_start=50 line_range_end=89 path=docs/long_running_sandbox.md git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/docs/long_running_sandbox.md#L50-L89"}​

## 2. Domain Model
| Model | Key Fields | Notes |
|-------|------------|-------|
| `SampleSubmission` | `id`, `sha256`, `source`, `priority`, `submitted_at`, `status` | Queue entry created by CLI/REST. |
| `SandboxLease` | `id`, `sandbox_id`, `connect_uri`, `vm_name`, `state`, `acquired_at`, `expires_at`, `health` | Tracks long-lived VM and adapter handle. |
| `SandboxCycle` | `id`, `sample_id`, `lease_id`, `state`, `requested_at`, `started_at`, `completed_at`, `exit_code`, `timed_out` | Represents one execution attempt against a lease. |
| `ExecutionSnapshot` | `cycle_id`, `stdout`, `stderr`, `duration`, `exit_code`, `timed_out` | Persisted for telemetry/REST. |
| `LogArtifact` | `id`, `cycle_id`, `kind`, `uri`, `content_type`, `parser_status`, `metadata` | Catalogs raw output and parsing metadata. |
| `Observable` | `id`, `cycle_id`, `type`, `value`, `first_seen`, `last_seen`, `context` | Normalized indicator extracted from artifacts. |
| `AllowListEntry` | `id`, `observable_id`, `scope`, `status`, `reason`, `expires_at`, `revision_id` | Versioned allow-list state per observable. |
| `AllowListRevision` | `id`, `cycle_id`, `previous_revision_id`, `created_at`, `changeset` | Groups allow-list diffs produced after a cycle. |

All models are serializable (Pydantic/dataclass) to simplify persistence, message passing, and REST serialization.

## 3. Core Services
- **`SampleService`** – accepts submissions, performs dedupe, and enqueues work.
- **`SandboxLeaseManager`** – provisions/refreshes leases, runs health probes, and exposes attach/detach APIs.​:codex-file-citation[codex-file-citation]{line_range_start=8 line_range_end=58 path=docs/long_running_sandbox.md git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/docs/long_running_sandbox.md#L8-L58"}​
- **`SandboxCycleService`** – drives the `pending → leasing → preparing → executing → post_processing → completed/failed` state machine and emits lifecycle events.​:codex-file-citation[codex-file-citation]{line_range_start=8 line_range_end=58 path=docs/long_running_sandbox.md git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/docs/long_running_sandbox.md#L8-L58"}​
- **`LogProcessingService`** – parses artifacts asynchronously, producing `Observable` entities with provenance.​:codex-file-citation[codex-file-citation]{line_range_start=50 line_range_end=58 path=docs/long_running_sandbox.md git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/docs/long_running_sandbox.md#L50-L58"}​
- **`AllowListService`** – classifies observables, issues revisions, and exports diffs to network adapters.​:codex-file-citation[codex-file-citation]{line_range_start=55 line_range_end=89 path=docs/long_running_sandbox.md git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/docs/long_running_sandbox.md#L55-L89"}​
- **`ApiFacade`** – thin application boundary used by both CLI and REST layers for submissions, status, and streaming.​:codex-file-citation[codex-file-citation]{line_range_start=60 line_range_end=89 path=docs/long_running_sandbox.md git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/docs/long_running_sandbox.md#L60-L89"}​
- **`NotificationService`** (optional) – emits webhooks or message-bus events for downstream systems.

Each service depends on repository interfaces (`SampleRepository`, `LeaseRepository`, `CycleRepository`, `ArtifactRepository`, `ObservableRepository`, `AllowListRepository`, `RevisionRepository`) to keep storage pluggable.

## 4. Workflow
1. Submission arrives via CLI/REST and is persisted as `SampleSubmission`.
2. Scheduler claims a lease through `SandboxLeaseManager`, ensuring health and renewing `expires_at`.
3. `SandboxCycleService.run_cycle()` transitions through states, delegating to executor/driver adapters for guest operations.
4. Raw artifacts are pushed to object storage and registered as `LogArtifact` records.
5. Background workers invoke `LogProcessingService` to extract `Observable` items (domains, IPs, URLs).
6. `AllowListService` filters for candidate C2 observables, generates `AllowListRevision`, and notifies network adapters.
7. REST/Webhook consumers receive updates on cycle completion and allow-list changes.

## 5. Persistence Layout
- **Relational DB** (PostgreSQL/SQLite): tables for submissions, cycles, leases, observables, allow-list entries, revisions, job locks.
- **Object Storage** (filesystem/S3): blob store for large artifacts (pcaps, guest logs), referenced by `LogArtifact.uri`.
- **Message Queue** (Redis/RabbitMQ) or async task runner for log parsing and allow-list promotion.

## 6. API Contract (excerpt)
- `POST /samples` → creates submission, returns `SampleSubmission`.
- `GET /cycles/{cycle_id}` → current state, execution snapshot, artifact links.
- `GET /observables?cycle_id=` → list of `Observable` records.
- `GET /allow-list` → active entries plus pending candidates.
- `GET /allow-list/revisions` → recent `AllowListRevision` items (for automation).
- Websocket/Webhook channel for cycle lifecycle events and allow-list updates.

## 7. Observability & Operations
- Structured logs per service with cycle/lease IDs.
- Metrics: queue depth, cycle durations, lease health, parser throughput.
- Traces for `run_cycle` pipeline to debug guest stalls.
- Disaster recovery: idempotent state transitions, resume non-terminal cycles on restart.​:codex-file-citation[codex-file-citation]{line_range_start=71 line_range_end=75 path=docs/long_running_sandbox.md git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/docs/long_running_sandbox.md#L71-L75"}​

## 8. Implementation Roadmap
1. Formalize domain models and repositories.
2. Wrap existing procedural scripts in adapter classes (libvirt executor, artifact capture) while preserving behaviour.
3. Introduce persistence and eventing.
4. Stand up REST API atop `ApiFacade`.
5. Add asynchronous log parsing and allow-list revision flows.
6. Expand observability and automation hooks.
