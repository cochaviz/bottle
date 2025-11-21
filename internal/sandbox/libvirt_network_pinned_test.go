package sandbox

import (
	"net"
	"testing"

	libvirt "libvirt.org/go/libvirt"
)

func TestListPinnedDHCPHostsParsesHosts(t *testing.T) {
	xml := `
<network>
  <ip family='ipv4' address='10.0.0.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='10.0.0.2' end='10.0.0.10'/>
      <host mac='52:54:00:00:00:01' ip='10.0.0.5'/>
      <host mac='52:54:00:00:00:02' ip='10.0.0.6'/>
    </dhcp>
  </ip>
</network>`

	restore := stubNetworkOps(t, xml, nil)
	defer restore()

	hosts, err := listPinnedDHCPHostsFromNetwork(&libvirt.Network{})
	if err != nil {
		t.Fatalf("listPinnedDHCPHostsFromNetwork unexpected error: %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}

	want := map[string]net.IP{
		"52:54:00:00:00:01": net.ParseIP("10.0.0.5"),
		"52:54:00:00:00:02": net.ParseIP("10.0.0.6"),
	}
	for _, host := range hosts {
		ip, ok := want[host.MAC]
		if !ok {
			t.Fatalf("unexpected host %s", host.MAC)
		}
		if !host.IP.Equal(ip) {
			t.Fatalf("host %s ip = %s want %s", host.MAC, host.IP, ip)
		}
	}
}
