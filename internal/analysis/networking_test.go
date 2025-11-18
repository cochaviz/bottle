package analysis

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"cochaviz/bottle/internal/sandbox"
)

func TestWhitelistIPAddsAndRemovesRules(t *testing.T) {
	lease := sandbox.SandboxLease{
		Metadata: map[string]any{
			"vm_ip": "10.13.37.42",
		},
	}

	nftSeq := &nftSequence{
		responses: []nftResponse{
			// ruleExists -> nat chain (no comment)
			{expected: []string{"list", "chain", natFamily, natTable, natChain}, output: ""},
			// add nat rule
			{expected: []string{"insert", "rule", natFamily, natTable, natChain, "position", "0", "ip", "saddr", "10.13.37.42", "ip", "daddr", "203.0.113.4", "counter", "accept", "comment", `"allow:10.13.37.42->203.0.113.4"`}},
			// add filter rule
			{expected: []string{"insert", "rule", filterFamily, filterTable, filterChain, "position", "0", "ip", "saddr", "10.13.37.42", "ip", "daddr", "203.0.113.4", "counter", "accept", "comment", `"allow:10.13.37.42->203.0.113.4"`}},
			// cleanup nat list handles
			{expected: []string{"-a", "list", "chain", natFamily, natTable, natChain}, output: `handle 5 comment "allow:10.13.37.42->203.0.113.4"`},
			// delete nat handle
			{expected: []string{"delete", "rule", natFamily, natTable, natChain, "handle", "5"}},
			// cleanup filter list handles
			{expected: []string{"-a", "list", "chain", filterFamily, filterTable, filterChain}, output: `handle 9 comment "allow:10.13.37.42->203.0.113.4"`},
			// delete filter handle
			{expected: []string{"delete", "rule", filterFamily, filterTable, filterChain, "handle", "9"}},
		},
	}
	restore := stubNFT(t, nftSeq)
	defer restore()

	cleanup, err := WhitelistIP(lease, "203.0.113.4")
	if err != nil {
		t.Fatalf("WhitelistIP unexpected error: %v", err)
	}
	if cleanup == nil {
		t.Fatalf("expected cleanup function")
	}
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
}

func TestWhitelistIPSkipsExistingRule(t *testing.T) {
	lease := sandbox.SandboxLease{
		Metadata: map[string]any{
			"vm_ip": "10.13.37.42",
		},
	}
	comment := `comment "allow:10.13.37.42->198.51.100.2"`

	nftSeq := &nftSequence{
		responses: []nftResponse{
			{expected: []string{"list", "chain", natFamily, natTable, natChain}, output: comment},
			{expected: []string{"list", "chain", filterFamily, filterTable, filterChain}, output: comment},
		},
	}
	restore := stubNFT(t, nftSeq)
	defer restore()

	cleanup, err := WhitelistIP(lease, "198.51.100.2")
	if err != nil {
		t.Fatalf("WhitelistIP unexpected error: %v", err)
	}
	if cleanup == nil {
		t.Fatalf("expected cleanup function")
	}
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup should be no-op, got error: %v", err)
	}
	if len(nftSeq.calls) != 2 {
		t.Fatalf("expected only list commands, got %d", len(nftSeq.calls))
	}
}

func TestWhitelistIPInvalidInput(t *testing.T) {
	lease := sandbox.SandboxLease{Metadata: map[string]any{}}
	if _, err := WhitelistIP(lease, ""); err == nil {
		t.Fatal("expected error for missing IP")
	}
	if _, err := WhitelistIP(lease, "not-an-ip"); err == nil {
		t.Fatal("expected error for invalid IP")
	}
	lease.Metadata["vm_ip"] = "   "
	if _, err := WhitelistIP(lease, "1.2.3.4"); err == nil {
		t.Fatal("expected error when vm_ip missing")
	}
}

func stubNFT(t *testing.T, seq *nftSequence) func() {
	t.Helper()
	prev := nftCommand
	nftCommand = func(args ...string) ([]byte, error) {
		return seq.next(args)
	}
	return func() {
		nftCommand = prev
	}
}

type nftResponse struct {
	expected []string
	output   string
	err      error
}

type nftSequence struct {
	index     int
	responses []nftResponse
	calls     [][]string
}

func (s *nftSequence) next(args []string) ([]byte, error) {
	if s.index >= len(s.responses) {
		return nil, errors.New("unexpected nft invocation")
	}
	resp := s.responses[s.index]
	s.index++
	s.calls = append(s.calls, append([]string(nil), args...))
	if resp.expected != nil {
		if strings.Join(resp.expected, " ") != strings.Join(args, " ") {
			return nil, fmt.Errorf("unexpected nft args: got %v want %v", args, resp.expected)
		}
	}
	if resp.err != nil {
		return nil, resp.err
	}
	return []byte(resp.output), nil
}
