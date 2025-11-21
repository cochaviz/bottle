package sandbox

import (
	"bytes"
	"crypto/sha1"
	"encoding/xml"
	"fmt"
	"net"
	"strings"

	libvirt "libvirt.org/go/libvirt"
)

const DefaultNetworkName = "lab_net"

var (
	describeNetworkXML = func(network *libvirt.Network) (string, error) {
		return network.GetXMLDesc(0)
	}
	fetchNetworkLeases = func(network *libvirt.Network) ([]libvirt.NetworkDHCPLease, error) {
		return network.GetDHCPLeases()
	}
	updateNetworkSection = func(network *libvirt.Network, cmd libvirt.NetworkUpdateCommand, section libvirt.NetworkUpdateSection, parentIndex int, xml string, flags libvirt.NetworkUpdateFlags) error {
		return network.Update(cmd, section, parentIndex, xml, flags)
	}
)

type NetworkLease struct {
	MAC string
	IP  net.IP
}

type libvirtNetwork interface {
	Acquire(mac string) (NetworkLease, error)
	Release(lease NetworkLease) error
	GetLeases() ([]NetworkLease, error)
}

type libvirtNetworkDriver struct {
	network *libvirt.Network
}

func newLibvirtNetworkDriver(network *libvirt.Network) libvirtNetwork {
	return &libvirtNetworkDriver{network: network}
}

type dhcpHost struct {
	MAC string
	IP  net.IP
}

type dhcpConfig struct {
	IPv4Ranges []ipRange
	Hosts      []dhcpHost
}

type ipRange struct {
	Start net.IP
	End   net.IP
}

type networkIPEntry struct {
	Address string `xml:"address,attr"`
	Netmask string `xml:"netmask,attr"`
	Prefix  string `xml:"prefix,attr"`
	Family  string `xml:"family,attr"`
	DHCP    struct {
		Ranges []struct {
			Start string `xml:"start,attr"`
			End   string `xml:"end,attr"`
		} `xml:"range"`
		Hosts []struct {
			MAC string `xml:"mac,attr"`
			IP  string `xml:"ip,attr"`
		} `xml:"host"`
	} `xml:"dhcp"`
}

func generateSandboxMAC(seed string) string {
	sum := sha1.Sum([]byte(seed))
	mac := []byte{0x52, 0x54, 0x00, sum[0], sum[1], sum[2]}
	// Set the locally administered bit and ensure unicast.
	mac[0] = (mac[0] | 0x02) & 0xfe
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
}

// ListNetworkLeases returns DHCP leases for the given libvirt network name.
func ListNetworkLeases(connectionURI, networkName string) ([]NetworkLease, error) {
	connectionURI = strings.TrimSpace(connectionURI)
	if connectionURI == "" {
		return nil, fmt.Errorf("connection URI is required")
	}
	networkName = strings.TrimSpace(networkName)
	if networkName == "" {
		return nil, fmt.Errorf("network name is required")
	}

	conn, err := libvirt.NewConnect(connectionURI)
	if err != nil {
		return nil, fmt.Errorf("open libvirt connection %s: %w", connectionURI, err)
	}
	defer conn.Close()

	network, err := conn.LookupNetworkByName(networkName)
	if err != nil {
		return nil, fmt.Errorf("lookup network %s: %w", networkName, err)
	}
	defer network.Free()

	driver := newLibvirtNetworkDriver(network)
	return driver.GetLeases()
}

// ListPinnedDHCPHosts returns the static DHCP host mappings configured on the network.
func ListPinnedDHCPHosts(connectionURI, networkName string) ([]NetworkLease, error) {
	connectionURI = strings.TrimSpace(connectionURI)
	if connectionURI == "" {
		return nil, fmt.Errorf("connection URI is required")
	}
	networkName = strings.TrimSpace(networkName)
	if networkName == "" {
		return nil, fmt.Errorf("network name is required")
	}

	conn, err := libvirt.NewConnect(connectionURI)
	if err != nil {
		return nil, fmt.Errorf("open libvirt connection %s: %w", connectionURI, err)
	}
	defer conn.Close()

	network, err := conn.LookupNetworkByName(networkName)
	if err != nil {
		return nil, fmt.Errorf("lookup network %s: %w", networkName, err)
	}
	defer network.Free()

	return listPinnedDHCPHostsFromNetwork(network)
}

func listPinnedDHCPHostsFromNetwork(network *libvirt.Network) ([]NetworkLease, error) {
	xmlDesc, err := describeNetworkXML(network)
	if err != nil {
		return nil, fmt.Errorf("describe network: %w", err)
	}
	cfg, err := parseNetworkDHCPConfig(xmlDesc)
	if err != nil {
		return nil, err
	}
	hosts := make([]NetworkLease, 0, len(cfg.Hosts))
	for _, host := range cfg.Hosts {
		if host.IP == nil {
			continue
		}
		hosts = append(hosts, NetworkLease{
			MAC: strings.ToLower(strings.TrimSpace(host.MAC)),
			IP:  host.IP,
		})
	}
	return hosts, nil
}

func (n *libvirtNetworkDriver) Acquire(mac string) (NetworkLease, error) {
	if n == nil || n.network == nil {
		return NetworkLease{}, fmt.Errorf("libvirt network handle is required")
	}
	if strings.TrimSpace(mac) == "" {
		return NetworkLease{}, fmt.Errorf("mac address is required")
	}

	xmlDesc, err := describeNetworkXML(n.network)
	if err != nil {
		return NetworkLease{}, fmt.Errorf("describe network: %w", err)
	}

	cfg, err := parseNetworkDHCPConfig(xmlDesc)
	if err != nil {
		return NetworkLease{}, err
	}
	if len(cfg.IPv4Ranges) == 0 {
		return NetworkLease{}, fmt.Errorf("network does not define an IPv4 DHCP range")
	}

	used := map[string]struct{}{}
	for _, host := range cfg.Hosts {
		if host.IP != nil {
			used[host.IP.String()] = struct{}{}
		}
	}

	leases, err := fetchNetworkLeases(n.network)
	if err != nil {
		return NetworkLease{}, fmt.Errorf("query DHCP leases: %w", err)
	}
	for _, lease := range leases {
		if ip := net.ParseIP(strings.TrimSpace(lease.IPaddr)); ip != nil {
			if v4 := ip.To4(); v4 != nil {
				used[v4.String()] = struct{}{}
			}
		}
	}

	ip, err := selectAvailableIP(cfg.IPv4Ranges, used)
	if err != nil {
		return NetworkLease{}, err
	}

	if err := removeDHCPHost(n.network, mac, ip.String()); err != nil {
		return NetworkLease{}, err
	}

	hostXML := fmt.Sprintf("<host mac='%s' ip='%s'/>", mac, ip.String())
	flags := libvirt.NETWORK_UPDATE_AFFECT_LIVE | libvirt.NETWORK_UPDATE_AFFECT_CONFIG
	if err := updateNetworkSection(n.network, libvirt.NETWORK_UPDATE_COMMAND_ADD_LAST, libvirt.NETWORK_SECTION_IP_DHCP_HOST, -1, hostXML, flags); err != nil {
		return NetworkLease{}, fmt.Errorf("pin DHCP lease: %w", err)
	}

	return NetworkLease{
		MAC: strings.ToLower(mac),
		IP:  ip,
	}, nil
}

func (n *libvirtNetworkDriver) Release(lease NetworkLease) error {
	if n == nil || n.network == nil {
		return fmt.Errorf("libvirt network handle is required")
	}
	ip := ""
	if lease.IP != nil {
		ip = lease.IP.String()
	}
	return removeDHCPHost(n.network, lease.MAC, ip)
}

func (n *libvirtNetworkDriver) GetLeases() ([]NetworkLease, error) {
	if n == nil || n.network == nil {
		return nil, fmt.Errorf("libvirt network handle is required")
	}
	rawLeases, err := fetchNetworkLeases(n.network)
	if err != nil {
		return nil, fmt.Errorf("query DHCP leases: %w", err)
	}
	var leases []NetworkLease
	for _, lease := range rawLeases {
		ip := net.ParseIP(strings.TrimSpace(lease.IPaddr))
		if ip == nil {
			continue
		}
		if v4 := ip.To4(); v4 == nil {
			continue
		}
		leases = append(leases, NetworkLease{
			MAC: strings.ToLower(strings.TrimSpace(lease.Mac)),
			IP:  ip.To4(),
		})
	}
	return leases, nil
}

func removeDHCPHost(network *libvirt.Network, mac, ip string) error {
	flags := libvirt.NETWORK_UPDATE_AFFECT_LIVE | libvirt.NETWORK_UPDATE_AFFECT_CONFIG
	if strings.TrimSpace(mac) != "" {
		hostXML := fmt.Sprintf("<host mac='%s'/>", mac)
		err := updateNetworkSection(network, libvirt.NETWORK_UPDATE_COMMAND_DELETE, libvirt.NETWORK_SECTION_IP_DHCP_HOST, -1, hostXML, flags)
		if err != nil && !isIgnorableDHCPDeleteError(err) {
			return fmt.Errorf("remove DHCP host (mac): %w", err)
		}
	}
	if strings.TrimSpace(ip) != "" {
		hostXML := fmt.Sprintf("<host ip='%s'/>", ip)
		err := updateNetworkSection(network, libvirt.NETWORK_UPDATE_COMMAND_DELETE, libvirt.NETWORK_SECTION_IP_DHCP_HOST, -1, hostXML, flags)
		if err != nil && !isIgnorableDHCPDeleteError(err) {
			return fmt.Errorf("remove DHCP host (ip): %w", err)
		}
	}
	return nil
}

func isIgnorableDHCPDeleteError(err error) bool {
	return isLibvirtError(err,
		libvirt.ERR_INVALID_ARG,
		libvirt.ERR_NO_DOMAIN,
		libvirt.ERR_OPERATION_INVALID,
	)
}

func selectAvailableIP(ranges []ipRange, used map[string]struct{}) (net.IP, error) {
	for _, r := range ranges {
		if r.Start == nil || r.End == nil {
			continue
		}
		cur := append(net.IP(nil), r.Start...)
		for ; compareIPs(cur, r.End) <= 0; incrementIP(cur) {
			if cur.To4() == nil {
				continue
			}
			if _, taken := used[cur.String()]; taken {
				continue
			}
			return append(net.IP(nil), cur...), nil
		}
	}
	return nil, fmt.Errorf("no available IPv4 addresses in DHCP range")
}

func compareIPs(a, b net.IP) int {
	a4 := a.To4()
	b4 := b.To4()
	if a4 == nil || b4 == nil {
		return 0
	}
	return bytes.Compare(a4, b4)
}

func incrementIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] != 0 {
			break
		}
	}
}

func parseNetworkDHCPConfig(xmlDesc string) (dhcpConfig, error) {
	var doc struct {
		IPs []networkIPEntry `xml:"ip"`
	}

	if err := xml.Unmarshal([]byte(xmlDesc), &doc); err != nil {
		return dhcpConfig{}, fmt.Errorf("parse network xml: %w", err)
	}

	cfg := dhcpConfig{}
	for _, ip := range doc.IPs {
		if !isIPv4Entry(ip) {
			continue
		}
		for _, rng := range ip.DHCP.Ranges {
			start := parseIPv4(rng.Start)
			end := parseIPv4(rng.End)
			if start == nil || end == nil {
				continue
			}
			if compareIPs(start, end) > 0 {
				start, end = end, start
			}
			cfg.IPv4Ranges = append(cfg.IPv4Ranges, ipRange{
				Start: start,
				End:   end,
			})
		}
		for _, h := range ip.DHCP.Hosts {
			if parsed := parseIPv4(h.IP); parsed != nil {
				cfg.Hosts = append(cfg.Hosts, dhcpHost{
					MAC: strings.ToLower(strings.TrimSpace(h.MAC)),
					IP:  parsed,
				})
			}
		}
	}

	return cfg, nil
}

func isIPv4Entry(entry networkIPEntry) bool {
	if strings.EqualFold(strings.TrimSpace(entry.Family), "ipv6") {
		return false
	}
	addr := parseIPv4(entry.Address)
	return addr != nil
}

func parseIPv4(value string) net.IP {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return nil
	}
	return ip.To4()
}
