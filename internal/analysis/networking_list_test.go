package analysis

import (
	"errors"
	"strings"
	"testing"
)

func TestListWhitelistedIPsParsesEntries(t *testing.T) {
	t.Cleanup(func() {
		nftCommand = realNftCommand
	})
	nftCommand = func(args ...string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "-a" && args[1] == "list" {
			output := `
table ip lab_nat {
  chain prerouting {
    ip saddr 10.0.0.2 ip daddr 203.0.113.4 counter accept comment "allow:10.0.0.2->203.0.113.4" # handle 23
  }
}
table inet lab_flt {
  chain forward {
    ip saddr 10.0.0.3 ip daddr 198.51.100.10 counter accept comment "allow:10.0.0.3->198.51.100.10" # handle 5
    ip saddr 10.0.0.2 ip daddr 203.0.113.4 counter accept comment "allow:10.0.0.2->203.0.113.4" # handle 6
  }
}`
			return []byte(output), nil
		}
		return nil, errors.New("unexpected args")
	}

	entries, err := ListWhitelistedIPs()
	if err != nil {
		t.Fatalf("ListWhitelistedIPs() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	got := map[string]struct{}{}
	for _, e := range entries {
		got[e.VMIP+"->"+e.DestIP] = struct{}{}
	}
	if _, ok := got["10.0.0.2->203.0.113.4"]; !ok {
		t.Fatalf("missing whitelist entry for 10.0.0.2->203.0.113.4: %#v", entries)
	}
	if _, ok := got["10.0.0.3->198.51.100.10"]; !ok {
		t.Fatalf("missing whitelist entry for 10.0.0.3->198.51.100.10: %#v", entries)
	}
}

func TestParseWhitelistComment(t *testing.T) {
	vm, dst, ok := parseWhitelistComment("allow:10.0.0.2->203.0.113.4")
	if !ok || vm != "10.0.0.2" || dst != "203.0.113.4" {
		t.Fatalf("parseWhitelistComment failed, got vm=%q dst=%q ok=%v", vm, dst, ok)
	}

	if _, _, ok := parseWhitelistComment("something else"); ok {
		t.Fatal("expected invalid parse to return ok=false")
	}

	if _, _, ok := parseWhitelistComment("allow:bad->1.1.1.1"); ok {
		t.Fatal("expected parse failure for invalid IP")
	}
	if _, _, ok := parseWhitelistComment("allow:1.1.1.1->bad"); ok {
		t.Fatal("expected parse failure for invalid IP")
	}
}

func TestExtractWhitelistComment(t *testing.T) {
	line := `ip saddr 10.0.0.2 ip daddr 203.0.113.4 counter accept comment "allow:10.0.0.2->203.0.113.4" # handle 6`
	comment := extractWhitelistComment(line)
	if !strings.HasPrefix(comment, "allow:") {
		t.Fatalf("expected allow comment, got %q", comment)
	}
	if comment != "allow:10.0.0.2->203.0.113.4" {
		t.Fatalf("unexpected comment: %q", comment)
	}
}
