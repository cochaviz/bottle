# bottle

`bottle` is a Go toolkit for building long-lived sandbox images and repeatedly running botnet or malware samples inside them.  It wraps libvirt, nftables, and a curated set of Debian-based VM specifications so you can build images, lease them for sandbox runs, launch short-lived analyses, or hand those runs off to a daemon for continuous monitoring.

## Features
- Opinionated sandbox builder that produces Debian Bookworm images for `amd64`, `i386`, `arm64`, `armhf`, and `ppc64el` (see `bottle sandbox list`)
- CLI workflows for one-off sandbox runs, single-shot analysis jobs, and a daemon that can orchestrate multiple analyses
- Pluggable command-line instrumentation rendered from Go templates so that helpers such as `tcpdump` can react to VM metadata (sample name, VM IP, interface, etc.)
- Hardened lab networking bootstrapper (`bottle setup`) that recreates nftables rules, bridges, and namespaces originally provided by shell scripts

## Requirements
- Linux host with KVM acceleration and libvirt/virt-install tooling (tested with Debian bookworm)
- `libvirtd` running locally and the user either root or a member of the `libvirt` and `kvm` groups
- `nft`, `ip`/`iproute2`, and privileges to create bridges, namespaces, and write to `/etc/bottle` & `/var/bottle` (required by `bottle setup`)
- Go 1.24+ for building the CLI
- Optional analysis helpers (`tcpdump`, custom scripts, etc.) referenced by instrumentation configs

## Installation
```bash
# Install the CLI
go install github.com/cochaviz/bottle/cmd/cli@latest

# Or build locally
git clone https://github.com/cochaviz/bottle.git
cd bottle
go build ./cmd/cli
```

## Storage layout
- Configuration: `/etc/bottle/config`
- Images: `/var/bottle/images`
- Build artifacts: `/var/bottle/artifacts`
- Temporary build roots: `/var/bottle/builds`
- Active sandbox leases and run state: `/var/bottle/leases`

Override directories with the `--image-dir`, `--artifact-dir`, or `--run-dir` flags whenever a command supports them.

## Quick start
```bash
# 1. Prepare lab networking (root only; may prompt for nftables/libvirt changes)
sudo bottle setup

# 2. Build at least one sandbox specification
sudo bottle sandbox build debian-bookworm-amd64

# 3. Confirm the image exists
sudo bottle sandbox list

# 4. Run an interactive sandbox worker (grabs a VM lease and keeps running)
sudo bottle sandbox run debian-bookworm-amd64 --sample-dir /srv/samples

# 5. Launch a single analysis for a sample, injecting a C2 address
sudo bottle analysis run /srv/samples/beacon.bin --c2 10.66.66.50
```

## CLI overview
- `bottle setup` – Creates/refreshes lab bridges, namespaces, nftables rules, and `/etc/bottle/config/networking.json`. Use `--clear` to remove the old config first.
- `bottle sandbox build <spec>` – Builds a VM image for a specification ID (see `bottle sandbox list`). Flags: `--image-dir`, `--artifact-dir`, `--connect-uri`.
- `bottle sandbox run <spec>` – Starts a worker that acquires a VM lease and keeps it running until interrupted. Flags: `--run-dir`, `--sample-dir`, `--domain`, `--connect-uri`.
- `bottle sandbox list` – Lists embedded specifications and whether an image exists locally.
- `bottle analysis run <sample>` – Runs a sample end-to-end. Automatically selects an image by architecture (or honor `--arch`), pushes files from `--sample-dir`, injects a C2 IP, and optionally starts instrumentation. Additional flags: `--sample-args`, `--instrumentation`, `--image-dir`, `--run-dir`, `--connect-uri`, `--sample-timeout`, `--sandbox-lifetime`. Each timeout flag is optional—setting it to `0` disables that safeguard.
- `bottle daemon serve` – Starts the daemon over a Unix socket (default `/var/run/bottle/daemon.sock`); use `bottle daemon start|stop|list` to interact with it from another terminal/host.

### Running analyses via the daemon
```bash
sudo bottle daemon serve --socket /var/run/bottle/daemon.sock &

# Schedule an analysis
bottle daemon start /srv/samples/beacon.bin --c2 10.66.66.50 --instrumentation configs/tcpdump.yaml

# List or stop analyses
bottle daemon list
bottle daemon stop <id>
```

The daemon accepts the same flag set as `analysis run` and is ideal for long-running automation or remote clients that only need socket access.
`bottle daemon list` now shows the sample path and C2 IP (when supplied via `--c2`) so you can see exactly which beacon each worker is pointing at before deciding to stop or restart it.

### Example systemd service
When you want the daemon to restart automatically on boot or after an unexpected crash, drop a service definition such as the one below into `/etc/systemd/system/bottled.service` (adjust the binary path, user, sockets, and directories for your host):

```ini
[Unit]
Description=Bottle sandbox daemon
After=network-online.target libvirtd.service
Requires=libvirtd.service

[Service]
Type=simple
User=root
Group=root
# Keep the socket path in one place so `ExecStart` stays readable.
Environment="BOTTLE_SOCKET=/var/run/bottle/daemon.sock"
ExecStart=/root/go/bin/bottle daemon serve --socket ${BOTTLE_SOCKET}
Restart=on-failure
RestartSec=5s
RuntimeDirectory=bottle
RuntimeDirectoryMode=0750

[Install]
WantedBy=multi-user.target
```

Reload systemd (`sudo systemctl daemon-reload`), then enable and start it with `sudo systemctl enable --now bottled`. Combine this with the CLI commands above to schedule analyses from any machine that can reach the daemon socket.

## Instrumentation
Instrumentation is defined in YAML files and rendered through Go templates with these variables:

| Variable | Meaning |
| --- | --- |
| `SampleName` | Base filename of the sample |
| `VmIp` | IP address assigned to the guest |
| `VmInterface` | Host-side tap interface for the lease |
| `C2Ip` | C2 IP supplied via `--c2` (if any) |
| `StartTime` | UTC start time (format `20060102T150405Z`) for embedding in filenames |
| `RunDir` | Filesystem path to the temporary lease directory for this run |
| `LogDir` | Dedicated log path (`/var/log/bottle/<SampleName>-<StartTime>` by default) where instrumentation helpers can emit artifacts |

StartTime, RunDir, and LogDir help you timestamp output and store artifacts near either the lease workspace or the persisted logs. `LogDir` is created after the worker starts (defaulting to `/var/log/bottle/<SampleName>-<StartTime>`) and is used as the instrumentation working directory, so any relative paths that your helpers emit land inside the log workspace while `RunDir` stays available if you need the sandbox state. Every run also drops an `analysis-config.json` file inside `LogDir` summarizing the sample path, arguments, C2 address, and timeout configuration for that execution.

Example CLI instrumentation (`configs/tcpdump.yaml`):

```yaml
cli:
  command: |
    mkdir -p /var/log/bottle
    exec tcpdump -i {{ .VmInterface }} -n -w /var/log/bottle/{{ .SampleName }}.pcap
```

Use the optional `output` key to control where CLI helpers emit their logs; defaults to `stdout`. Setting `output: file` writes both stdout and stderr to `<LogDir>/<instrumentation_name>-<pid>.log` (where the instrumentation name is derived from the command’s basename), and it keeps the working directory inside `LogDir` so helpers can drop artifacts there.

You can also layer multiple CLI helpers and Suricata sensors in a single file and pass it to `--instrumentation`:

```yaml
cli:
    - command: tcpdump -i {{ .VmInterface }} -w /home/user/{{ .SampleName }}.pcap host {{ .VmIp }} and host {{ .C2Ip }}
      output: file
    - command: gomon {{ .VmInterface }} {{ .VmIp }} --c2-ip {{ .C2Ip }} --sample-id {{ .SampleName }} --save-packets 100
      output: stdout
suricata:
    - config: /home/user/suricata.yml
      binary: /usr/bin/suricata
      output: file
```

Pass the instrumentation config with `--instrumentation instrumentation.yaml`. `bottle` spawns each command with the rendered template, streams stdout/stderr to the console, and terminates them when the analysis finishes.

### Suricata instrumentation
Suricata configs are rendered and injected via `suricata` YAML blocks. The instrumentation writes the templated file to a temporary location, starts Suricata with `suricata -c <rendered> -i <VmInterface>`, and removes the generated file when the instrumentation stops. Provide the template path and optionally override the Suricata binary so the template can reference your lab-specific helpers.

```yaml
suricata:
  config: configs/suricata.yaml.tmpl
  binary: /usr/local/bin/suricata # optional; defaults to `suricata`
```

The templated Suricata config gains access to the same instrumentation variables (`SampleName`, `VmIp`, `VmInterface`, `C2Ip`, `StartTime`, `RunDir`, `LogDir`) so you can inline the metadata directly in your YAML. Set `output: file` to capture Suricata’s stdout/stderr in `LogDir/suricata-<pid>.log`; the instrumentation still writes the rendered config to a temporary file before dropping it in place. Use camelCase keys only when referencing the data inside templates (e.g., `{{ .SampleName }}`).

## Development
- Format & lint using `go fmt` / `golangci-lint` (not vendored)
- Run the tests with:
  ```bash
  go test ./...
  ```
- Module path: `github.com/cochaviz/bottle`
- The primary CLI lives under `cmd/cli`, runtime wiring is in `config/simple`, and lab tooling is under `internal/setup`

Contributions should avoid touching `/etc` or `/var` paths unless you explicitly run `bottle setup` as root on a host you control.
