package sandbox

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	libvirt "libvirt.org/go/libvirt"
)

const (
	// GuestSetupMountPath is the directory inside the guest where setup artifacts are mounted.
	GuestSetupMountPath = "/mnt/mime_setup"
	// GuestSampleMountPath is the directory inside the guest where sample artifacts are mounted.
	GuestSampleMountPath = "/mnt/mime_sample"
	guestMountTimeout    = 2 * time.Minute
)

type guestCommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type guestExecRequest struct {
	Execute   string             `json:"execute"`
	Arguments guestExecArguments `json:"arguments"`
}

type guestExecArguments struct {
	Path          string   `json:"path"`
	Arg           []string `json:"arg"`
	CaptureOutput bool     `json:"capture-output"`
}

type guestExecResponse struct {
	Return struct {
		PID int `json:"pid"`
	} `json:"return"`
}

type guestExecStatusRequest struct {
	Execute   string                   `json:"execute"`
	Arguments guestExecStatusArguments `json:"arguments"`
}

type guestExecStatusArguments struct {
	PID int `json:"pid"`
}

type guestExecStatusResponse struct {
	Return guestExecStatusResult `json:"return"`
}

type guestExecStatusResult struct {
	Exited   bool   `json:"exited"`
	ExitCode int    `json:"exitcode"`
	OutData  string `json:"out-data"`
	ErrData  string `json:"err-data"`
}

func (d *LibvirtDriver) configureGuestMounts(domain *libvirt.Domain, lease *SandboxLease, setupEntries []setupFileEntry, logger *slog.Logger) (string, string, error) {
	needSetup := len(setupEntries) > 0
	needSample := strings.TrimSpace(lease.Specification.SampleDir) != ""
	if !needSetup && !needSample {
		return "", "", nil
	}

	if err := waitForGuestAgent(domain, 5*time.Second, 24); err != nil {
		return "", "", fmt.Errorf("wait for guest agent: %w", err)
	}

	script := buildGuestMountScript(needSetup, needSample)
	result, err := runGuestShellCommand(domain, script, guestMountTimeout)
	if err != nil {
		return "", "", fmt.Errorf("configure guest mounts: %w", err)
	}

	var mounts struct {
		Setup  string `json:"setup"`
		Sample string `json:"sample"`
	}
	output := strings.TrimSpace(result.Stdout)
	if output == "" {
		return "", "", errors.New("guest mount discovery produced no output")
	}
	if err := json.Unmarshal([]byte(output), &mounts); err != nil {
		return "", "", fmt.Errorf("parse guest mount discovery output: %w", err)
	}

	if needSetup && strings.TrimSpace(mounts.Setup) == "" {
		return "", "", errors.New("setup disk not detected inside guest")
	}
	if needSample && strings.TrimSpace(mounts.Sample) == "" {
		return "", "", errors.New("sample disk not detected inside guest")
	}

	if lease.Metadata == nil {
		lease.Metadata = map[string]any{}
	}
	if setupPath := strings.TrimSpace(mounts.Setup); setupPath != "" {
		lease.Metadata["setup_mount_path"] = setupPath
		logger.Info("detected setup disk", "guest_path", setupPath)
	}
	if samplePath := strings.TrimSpace(mounts.Sample); samplePath != "" {
		lease.Metadata["sample_mount_path"] = samplePath
		logger.Info("detected sample disk", "guest_path", samplePath)
	}
	return strings.TrimSpace(mounts.Setup), strings.TrimSpace(mounts.Sample), nil
}

func waitForGuestAgent(domain *libvirt.Domain, interval time.Duration, retries int) error {
	if retries <= 0 {
		retries = 1
	}
	if interval <= 0 {
		interval = time.Second
	}

	request := `{"execute":"guest-info"}`
	for i := 0; i < retries; i++ {
		if _, err := domain.QemuAgentCommand(
			request,
			libvirt.DOMAIN_QEMU_AGENT_COMMAND_DEFAULT,
			0,
		); err == nil {
			return nil
		}
		time.Sleep(interval)
	}
	return fmt.Errorf("timeout waiting for guest agent after %d attempts", retries)
}

func buildGuestMountScript(needSetup, needSample bool) string {
	boolToInt := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	return fmt.Sprintf(`set -eu
need_setup=%d
need_sample=%d
setup_mount='%s'
sample_mount='%s'
setup_path=""
sample_path=""
umount "$setup_mount" >/dev/null 2>&1 || true
umount "$sample_mount" >/dev/null 2>&1 || true
mkdir -p "$setup_mount" "$sample_mount"
for dev in $(lsblk -nrpo NAME,TYPE 2>/dev/null | awk '$2 == "rom" { print $1 }'); do
    [ -b "$dev" ] || continue
    tmp="$(mktemp -d)"
    if mount -o ro "$dev" "$tmp" >/dev/null 2>&1; then
        if [ "$need_setup" -eq 1 ] && [ -z "$setup_path" ] && [ -f "$tmp/setup" ]; then
            umount "$tmp" >/dev/null 2>&1 || true
            rmdir "$tmp"
            mount -o ro "$dev" "$setup_mount"
            setup_path="$setup_mount"
            continue
        fi
        if [ "$need_sample" -eq 1 ] && [ -z "$sample_path" ]; then
            umount "$tmp" >/dev/null 2>&1 || true
            rmdir "$tmp"
            mount -o ro "$dev" "$sample_mount"
            sample_path="$sample_mount"
            continue
        fi
        umount "$tmp" >/dev/null 2>&1 || true
    fi
    rmdir "$tmp"
done
printf '{"setup":"%%s","sample":"%%s"}' "$setup_path" "$sample_path"
`, boolToInt(needSetup), boolToInt(needSample), GuestSetupMountPath, GuestSampleMountPath)
}

func runGuestShellCommand(domain *libvirt.Domain, script string, timeout time.Duration) (guestCommandResult, error) {
	return runGuestCommand(domain, "/bin/sh", []string{"-c", script}, timeout)
}

func runGuestCommand(domain *libvirt.Domain, path string, args []string, timeout time.Duration) (guestCommandResult, error) {
	if strings.TrimSpace(path) == "" {
		return guestCommandResult{}, errors.New("guest command path is required")
	}
	if timeout <= 0 {
		timeout = guestMountTimeout
	}
	if args == nil {
		args = []string{}
	}

	req := guestExecRequest{
		Execute: "guest-exec",
		Arguments: guestExecArguments{
			Path:          path,
			Arg:           args,
			CaptureOutput: true,
		},
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return guestCommandResult{}, fmt.Errorf("marshal guest exec request: %w", err)
	}

	resp, err := domain.QemuAgentCommand(string(payload), libvirt.DOMAIN_QEMU_AGENT_COMMAND_DEFAULT, 0)
	if err != nil {
		return guestCommandResult{}, fmt.Errorf("invoke guest exec: %w", err)
	}

	var execResp guestExecResponse
	if err := json.Unmarshal([]byte(resp), &execResp); err != nil {
		return guestCommandResult{}, fmt.Errorf("decode guest exec response: %w", err)
	}
	if execResp.Return.PID == 0 {
		return guestCommandResult{}, errors.New("guest exec returned invalid pid")
	}

	return waitForGuestCommand(domain, execResp.Return.PID, timeout)
}

func waitForGuestCommand(domain *libvirt.Domain, pid int, timeout time.Duration) (guestCommandResult, error) {
	deadline := time.Now().Add(timeout)
	req := guestExecStatusRequest{
		Execute: "guest-exec-status",
		Arguments: guestExecStatusArguments{
			PID: pid,
		},
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return guestCommandResult{}, fmt.Errorf("marshal guest exec status request: %w", err)
	}

	for {
		resp, err := domain.QemuAgentCommand(string(payload), libvirt.DOMAIN_QEMU_AGENT_COMMAND_DEFAULT, 0)
		if err != nil {
			return guestCommandResult{}, fmt.Errorf("query guest exec status: %w", err)
		}

		var status guestExecStatusResponse
		if err := json.Unmarshal([]byte(resp), &status); err != nil {
			return guestCommandResult{}, fmt.Errorf("decode guest exec status: %w", err)
		}

		if status.Return.Exited {
			result := guestCommandResult{
				ExitCode: status.Return.ExitCode,
				Stdout:   decodeBase64(status.Return.OutData),
				Stderr:   decodeBase64(status.Return.ErrData),
			}
			if status.Return.ExitCode != 0 {
				return result, fmt.Errorf("guest command exit code %d: %s", status.Return.ExitCode, strings.TrimSpace(result.Stderr))
			}
			return result, nil
		}

		if time.Now().After(deadline) {
			return guestCommandResult{}, errors.New("guest command timed out")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func decodeBase64(data string) string {
	if strings.TrimSpace(data) == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return ""
	}
	return string(decoded)
}
