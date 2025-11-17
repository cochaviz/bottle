package sandbox

import (
	"net"
	"strings"
	"testing"

	libvirt "libvirt.org/go/libvirt"
)

func TestSelectAvailableIP(t *testing.T) {
	ranges := []ipRange{
		{Start: net.ParseIP("10.0.0.2"), End: net.ParseIP("10.0.0.5")},
	}
	used := map[string]struct{}{
		"10.0.0.2": {},
		"10.0.0.3": {},
	}
	ip, err := selectAvailableIP(ranges, used)
	if err != nil {
		t.Fatalf("selectAvailableIP unexpected error: %v", err)
	}
	if expected := "10.0.0.4"; ip.String() != expected {
		t.Fatalf("expected %s, got %s", expected, ip.String())
	}
}

func TestSelectAvailableIPExhausted(t *testing.T) {
	ranges := []ipRange{
		{Start: net.ParseIP("10.0.0.2"), End: net.ParseIP("10.0.0.3")},
	}
	used := map[string]struct{}{
		"10.0.0.2": {},
		"10.0.0.3": {},
	}
	if _, err := selectAvailableIP(ranges, used); err == nil {
		t.Fatal("expected error when DHCP range is exhausted")
	}
}

func TestParseNetworkDHCPConfig(t *testing.T) {
	xml := `
<network>
  <ip family='ipv4' address='10.0.0.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='10.0.0.2' end='10.0.0.10'/>
      <host mac='52:54:00:00:00:01' ip='10.0.0.5'/>
    </dhcp>
  </ip>
  <ip family='ipv6' address='fd00::1'>
    <dhcp>
      <range start='fd00::2' end='fd00::10'/>
    </dhcp>
  </ip>
</network>`
	cfg, err := parseNetworkDHCPConfig(xml)
	if err != nil {
		t.Fatalf("parseNetworkDHCPConfig unexpected error: %v", err)
	}
	if len(cfg.IPv4Ranges) != 1 {
		t.Fatalf("expected 1 IPv4 range, got %d", len(cfg.IPv4Ranges))
	}
	if len(cfg.Hosts) != 1 {
		t.Fatalf("expected 1 host entry, got %d", len(cfg.Hosts))
	}
	if cfg.Hosts[0].IP.String() != "10.0.0.5" {
		t.Fatalf("expected host IP 10.0.0.5, got %s", cfg.Hosts[0].IP.String())
	}
}

func TestLibvirtNetworkAcquire(t *testing.T) {
	netXML := `
<network>
  <ip family='ipv4' address='10.0.0.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='10.0.0.2' end='10.0.0.4'/>
      <host mac='52:54:00:00:00:aa' ip='10.0.0.2'/>
    </dhcp>
  </ip>
</network>`

	restore := stubNetworkOps(t, netXML, []libvirt.NetworkDHCPLease{
		{IPaddr: "10.0.0.3"},
	})
	defer restore()

	driver := newLibvirtNetworkDriver(&libvirt.Network{})
	lease, err := driver.Acquire("52:54:00:00:00:bb")
	if err != nil {
		t.Fatalf("Acquire unexpected error: %v", err)
	}
	if lease.IP.String() != "10.0.0.4" {
		t.Fatalf("expected lease 10.0.0.4, got %s", lease.IP.String())
	}
	if len(testNetworkUpdates) == 0 {
		t.Fatalf("expected network updates")
	}
	addFound := false
	for _, upd := range testNetworkUpdates {
		if upd.cmd == libvirt.NETWORK_UPDATE_COMMAND_ADD_LAST && strings.Contains(upd.xml, "10.0.0.4") {
			addFound = true
		}
	}
	if !addFound {
		t.Fatalf("expected DHCP host addition")
	}
}

func TestLibvirtNetworkRelease(t *testing.T) {
	restore := stubNetworkOps(t, "", nil)
	defer restore()

	driver := newLibvirtNetworkDriver(&libvirt.Network{})
	lease := NetworkLease{
		MAC: "52:54:00:00:00:bb",
		IP:  net.ParseIP("10.0.0.5"),
	}
	if err := driver.Release(lease); err != nil {
		t.Fatalf("Release unexpected error: %v", err)
	}
	if len(testNetworkUpdates) == 0 {
		t.Fatalf("expected delete update")
	}
	if testNetworkUpdates[0].cmd != libvirt.NETWORK_UPDATE_COMMAND_DELETE {
		t.Fatalf("expected delete command, got %v", testNetworkUpdates[0].cmd)
	}
}

type fakeNetworkUpdate struct {
	cmd     libvirt.NetworkUpdateCommand
	section libvirt.NetworkUpdateSection
	xml     string
	flags   libvirt.NetworkUpdateFlags
}

var testNetworkUpdates []fakeNetworkUpdate

func stubNetworkOps(t *testing.T, xml string, leases []libvirt.NetworkDHCPLease) func() {
	t.Helper()
	testNetworkUpdates = nil

	prevDescribe := describeNetworkXML
	prevLeases := fetchNetworkLeases
	prevUpdate := updateNetworkSection

	describeNetworkXML = func(*libvirt.Network) (string, error) {
		if xml == "" {
			return "<network/>", nil
		}
		return xml, nil
	}
	fetchNetworkLeases = func(*libvirt.Network) ([]libvirt.NetworkDHCPLease, error) {
		return append([]libvirt.NetworkDHCPLease(nil), leases...), nil
	}
	updateNetworkSection = func(_ *libvirt.Network, cmd libvirt.NetworkUpdateCommand, section libvirt.NetworkUpdateSection, parentIndex int, xml string, flags libvirt.NetworkUpdateFlags) error {
		testNetworkUpdates = append(testNetworkUpdates, fakeNetworkUpdate{
			cmd:     cmd,
			section: section,
			xml:     xml,
			flags:   flags,
		})
		return nil
	}

	return func() {
		describeNetworkXML = prevDescribe
		fetchNetworkLeases = prevLeases
		updateNetworkSection = prevUpdate
		testNetworkUpdates = nil
	}
}
