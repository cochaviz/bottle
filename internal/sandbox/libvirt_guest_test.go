package sandbox

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	libvirt "libvirt.org/go/libvirt"
)

type stubQemuAgentDomain struct {
	responses []guestExecStatusResponse
	call      int
}

func (d *stubQemuAgentDomain) QemuAgentCommand(_ string, _ libvirt.DomainQemuAgentCommandTimeout, _ uint32) (string, error) {
	if len(d.responses) == 0 {
		return "", errors.New("no responses configured")
	}
	idx := d.call
	if idx >= len(d.responses) {
		idx = len(d.responses) - 1
	}
	d.call++

	payload, err := json.Marshal(d.responses[idx])
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func TestWaitForGuestCommand_NoTimeoutWhenZero(t *testing.T) {
	domain := &stubQemuAgentDomain{
		responses: []guestExecStatusResponse{
			{Return: guestExecStatusResult{Exited: false}},
			{
				Return: guestExecStatusResult{
					Exited:   true,
					ExitCode: 0,
					OutData:  base64.StdEncoding.EncodeToString([]byte("ok")),
				},
			},
		},
	}

	result, err := waitForGuestCommand(domain, 123, 0)
	if err != nil {
		t.Fatalf("waitForGuestCommand returned error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Stdout != "ok" {
		t.Fatalf("expected stdout %q, got %q", "ok", result.Stdout)
	}
}

func TestWaitForGuestCommand_TimesOut(t *testing.T) {
	domain := &stubQemuAgentDomain{
		responses: []guestExecStatusResponse{
			{Return: guestExecStatusResult{Exited: false}},
		},
	}

	_, err := waitForGuestCommand(domain, 123, 200*time.Millisecond)
	if !errors.Is(err, ErrGuestCommandTimedOut) {
		t.Fatalf("expected timeout error, got %v", err)
	}
}
