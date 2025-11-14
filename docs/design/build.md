# Sandbox Image Build Pipeline Design

## 1. Motivation
- The current `talkbox.sandbox.build` module mirrors a shell script; a structured pipeline enables reproducible image builds, artifact cataloging, and audit history.​:codex-file-citation[codex-file-citation]{line_range_start=1 line_range_end=200 path=talkbox/sandbox/build.py git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/talkbox/sandbox/build.py#L1-L200"}​
- Long-running sandboxes depend on predictable base images tied to profiles and allow-list revisions.

## 2. Roles & Models
| Model | Purpose |
|-------|---------|
| `SandboxImageSpec` | Declarative description (OS release, packages, hardening, network layout, installer assets). |
| `SandboxImage` | Immutable record of a built artifact (spec version, qcow2 URI, checksum, build timestamp). |
| `ImageBuildRun` | Audit of a single build attempt (inputs, logs, status, produced artifacts, builder host). |
| `BuildArtifact` | Companion outputs (cloud-init seed, manifest, packer logs). |
| `BuildQueueItem` | Task enqueued by CLI/API to request a build or rebuild. |

## 3. Pipeline Stages
1. **Spec Resolution**
   - Load `SandboxImageSpec` from repository (YAML/Pydantic); merge profile defaults (`BuildProfile`, `RunProfile`).​:codex-file-citation[codex-file-citation]{line_range_start=67 line_range_end=199 path=talkbox/sandbox/profiles.py git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/talkbox/sandbox/profiles.py#L67-L199"}​
2. **Environment Preparation**
   - Ensure build network exists via libvirt (`_ensure_network`), allocate temporary storage, fetch kernel/initrd assets.​:codex-file-citation[codex-file-citation]{line_range_start=160 line_range_end=200 path=talkbox/sandbox/build.py git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/talkbox/sandbox/build.py#L160-L200"}​
3. **Guest Provisioning**
   - Invoke builder adapter (virt-install or Packer) with resolved kernel args, preseed/cloud-init, disk size, CPU topology.
4. **Post-Build Verification**
   - Boot the image in headless mode, run smoke tests (guest agent, network reachability, INetSim connectivity).
5. **Artifact Publication**
   - Upload qcow2 and auxiliary files to artifact store, compute checksums, generate manifest.
6. **Catalog Update**
   - Persist `SandboxImage` and `ImageBuildRun` records; mark previous images as superseded if spec version bumped.
7. **Notification**
   - Notify orchestration services to refresh pools or schedule lease rollouts.

## 4. Services & Adapters
- `ImageSpecRepository` – CRUD for specs, supports versioning/migrations.
- `ImageRepository` – stores built image metadata and retrieval helpers.
- `BuildArtifactStore` – writes outputs to filesystem/S3 with retention policies.
- `ImageBuilderService` – orchestrates stages, records `ImageBuildRun`, integrates with `SandboxLeaseManager` for rollouts.
- `BuilderAdapter` implementations:
  - `VirtInstallBuilder` (wraps existing Python logic).
  - Optional `PackerBuilder` for future flexibility.
- `SmokeTestRunner` – executes guest validation scripts.

## 5. CLI & API Touchpoints
- CLI: `python -m talkbox sandbox build --profile <profile> --spec <spec_id>` (reuses `BuildConfig` defaults).​:codex-file-citation[codex-file-citation]{line_range_start=204 line_range_end=283 path=talkbox/cli.py git_url="https://github.com/cochaviz/talkbox/blob/profile-refactor/talkbox/cli.py#L204-L283"}​
- REST: `POST /sandbox-images` (enqueue build), `GET /sandbox-images/{id}` (status), `GET /sandbox-images?spec=...` (list versions).
- Webhooks for build completion or failure.

## 6. Data Persistence
- Tables: `image_specs`, `sandbox_images`, `image_build_runs`, `build_artifacts`.
- Enforce foreign-key relationship between `sandbox_images.spec_id` and `image_specs.id`.
- Store raw logs (virt-install stdout/stderr) in artifact store and reference path.

## 7. Security & Compliance
- Isolate build network with outbound-only access; reuse existing `build_net.xml`.
- Record base package manifest and vulnerability scan results per build.
- Sign manifests/checksums for tamper detection.

## 8. Rollout Strategy
- Tag images with semantic version derived from spec + build number.
- Allow orchestration layer to pin leases to specific image versions during staged rollouts.
- Provide rollback path: mark previous stable image as active if smoke tests fail.

## 9. Implementation Phases
1. Model & repository scaffolding.
2. Adapter abstraction around current build script.
3. Smoke test automation.
4. REST/CLI enhancements for build orchestration.
5. Integration with lease manager to schedule rolling updates.

