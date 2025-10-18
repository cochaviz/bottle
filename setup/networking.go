package setup

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

// NftTemplateConfig holds values injected into nftables template files.
type NftTemplateConfig struct {
	DNSAllowlist []string
	DNSRate      string
	DNSBurst     string
}

// Config captures the parameters required to prepare the lab network.
type Config struct {
	LabBridge         string
	LabGatewayCIDR    string
	VMNetworkCIDR     string
	InetBridge        string
	Namespace         string
	VethHost          string
	VethNamespace     string
	InetGatewayCIDR   string
	InetNamespaceCIDR string
	NftFiles          []string
	NftTemplate       NftTemplateConfig
}

// DefaultConfig mirrors the original shell script defaults.
var DefaultConfig = Config{
	LabBridge:         "br_lab",
	LabGatewayCIDR:    "10.13.37.1/24",
	VMNetworkCIDR:     "10.13.37.0/24",
	InetBridge:        "br_inet",
	Namespace:         "inetsim",
	VethHost:          "veth-inet-br",
	VethNamespace:     "veth-inet-ns",
	InetGatewayCIDR:   "10.66.66.1/24",
	InetNamespaceCIDR: "10.66.66.2/24",
	NftTemplate: NftTemplateConfig{
		DNSAllowlist: []string{"1.1.1.1", "8.8.8.8"},
		DNSRate:      "60/minute",
		DNSBurst:     "30 packets",
	},
}

//go:embed lab_net.nft
var labNetRules string

func loadConfig() (Config, error) {
	path := filepath.Join(ConfigDir, "networking.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := writeConfigFile(DefaultConfig); err != nil {
				return Config{}, err
			}
			return DefaultConfig, nil
		}
		return Config{}, fmt.Errorf("read persisted config: %w", err)
	}
	var persisted Config
	if err := json.Unmarshal(data, &persisted); err != nil {
		return Config{}, fmt.Errorf("decode persisted config: %w", err)
	}
	return persisted, nil
}

// Setup provisions the lab networking environment.
func SetupNetwork(ctx context.Context) error {
	if err := requireRoot(); err != nil {
		return err
	}
	if err := ensureCommands("nft"); err != nil {
		return err
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	parsed, err := parseAddresses(cfg)
	if err != nil {
		return err
	}

	if err := ensureLabBridge(parsed); err != nil {
		return err
	}
	if err := ensureInetBridge(parsed); err != nil {
		return err
	}
	if err := ensureNamespace(parsed); err != nil {
		return err
	}
	if err := configureSysctls(ctx); err != nil {
		return err
	}
	if err := programNftables(ctx, parsed); err != nil {
		return err
	}
	return nil
}

type parsedConfig struct {
	Config
	labGateway        *netlink.Addr
	vmNetwork         *net.IPNet
	inetBridgeAddr    *netlink.Addr
	inetNamespaceAddr *netlink.Addr
	inetGateway       *netlink.Addr
}

type nftTemplateData struct {
	VMNetworkCIDR   string
	InetNetworkCIDR string
	InetSimIP       string
	InetBridge      string
	DNSAllowlist    []string
	DNSRate         string
	DNSBurst        string
}

func parseAddresses(cfg Config) (parsedConfig, error) {
	labGW, err := netlink.ParseAddr(cfg.LabGatewayCIDR)
	if err != nil {
		return parsedConfig{}, fmt.Errorf("parse lab gateway: %w", err)
	}
	_, vmNet, err := net.ParseCIDR(cfg.VMNetworkCIDR)
	if err != nil {
		return parsedConfig{}, fmt.Errorf("parse VM network: %w", err)
	}
	inetBridgeAddr, err := netlink.ParseAddr(cfg.InetGatewayCIDR)
	if err != nil {
		return parsedConfig{}, fmt.Errorf("parse INet bridge addr: %w", err)
	}
	inetNamespaceAddr, err := netlink.ParseAddr(cfg.InetNamespaceCIDR)
	if err != nil {
		return parsedConfig{}, fmt.Errorf("parse INet namespace addr: %w", err)
	}

	return parsedConfig{
		Config:            cfg,
		labGateway:        labGW,
		vmNetwork:         vmNet,
		inetBridgeAddr:    inetBridgeAddr,
		inetNamespaceAddr: inetNamespaceAddr,
		inetGateway:       inetBridgeAddr,
	}, nil
}

func buildNftTemplateData(cfg parsedConfig) (nftTemplateData, error) {
	if cfg.vmNetwork == nil {
		return nftTemplateData{}, errors.New("vm network not configured")
	}
	if cfg.inetNamespaceAddr == nil || cfg.inetNamespaceAddr.IP == nil {
		return nftTemplateData{}, errors.New("inet namespace address not configured")
	}
	data := nftTemplateData{
		VMNetworkCIDR: cfg.vmNetwork.String(),
		InetSimIP:     cfg.inetNamespaceAddr.IP.String(),
		InetBridge:    cfg.InetBridge,
		DNSAllowlist:  append([]string(nil), cfg.NftTemplate.DNSAllowlist...),
		DNSRate:       cfg.NftTemplate.DNSRate,
		DNSBurst:      cfg.NftTemplate.DNSBurst,
	}

	if cfg.inetNamespaceAddr.IPNet != nil {
		ipNet := *cfg.inetNamespaceAddr.IPNet
		if ipNet.IP != nil {
			ipCopy := append(net.IP(nil), ipNet.IP...)
			ipNet.IP = ipCopy.Mask(ipNet.Mask)
			data.InetNetworkCIDR = ipNet.String()
		}
	}
	if data.InetNetworkCIDR == "" && cfg.inetBridgeAddr != nil && cfg.inetBridgeAddr.IPNet != nil {
		ipNet := *cfg.inetBridgeAddr.IPNet
		if ipNet.IP != nil {
			ipCopy := append(net.IP(nil), ipNet.IP...)
			ipNet.IP = ipCopy.Mask(ipNet.Mask)
			data.InetNetworkCIDR = ipNet.String()
		}
	}
	if data.InetNetworkCIDR == "" {
		if _, inetNet, err := net.ParseCIDR(cfg.Config.InetGatewayCIDR); err == nil {
			ipCopy := append(net.IP(nil), inetNet.IP...)
			inetNet.IP = ipCopy.Mask(inetNet.Mask)
			data.InetNetworkCIDR = inetNet.String()
		}
	}
	if data.InetNetworkCIDR == "" {
		return nftTemplateData{}, errors.New("unable to determine INetSim network CIDR")
	}
	return data, nil
}

func requireRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("run me as root")
	}
	return nil
}

func ensureCommands(names ...string) error {
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("%s not found: %w", name, err)
		}
	}
	return nil
}

func warnForwardModeNone(xmlPath string) bool {
	content, err := os.ReadFile(xmlPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(content), "forward mode='none'") || strings.Contains(string(content), "forward mode=\"none\"")
}

func ensureLabBridge(cfg parsedConfig) error {
	log.Printf("[*] Waiting for %s to appear…", cfg.LabBridge)
	var link netlink.Link
	var err error
	for i := 0; i < 20; i++ {
		link, err = netlink.LinkByName(cfg.LabBridge)
		if err == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("bridge %s not found: %w", cfg.LabBridge, err)
	}
	if err := ensureAddress(link, cfg.labGateway); err != nil {
		return err
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("bring %s up: %w", cfg.LabBridge, err)
	}
	return nil
}

func ensureInetBridge(cfg parsedConfig) error {
	log.Printf("[*] Creating %s…", cfg.InetBridge)
	link, err := netlink.LinkByName(cfg.InetBridge)
	if err != nil {
		if isLinkNotFound(err) {
			br := &netlink.Bridge{
				LinkAttrs: netlink.LinkAttrs{
					Name: cfg.InetBridge,
				},
			}
			if err := netlink.LinkAdd(br); err != nil && !errors.Is(err, syscall.EEXIST) {
				return fmt.Errorf("create bridge %s: %w", cfg.InetBridge, err)
			}
			link, err = netlink.LinkByName(cfg.InetBridge)
		}
		if err != nil {
			return fmt.Errorf("get bridge %s: %w", cfg.InetBridge, err)
		}
	}
	if err := ensureAddress(link, cfg.inetBridgeAddr); err != nil {
		return err
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("bring %s up: %w", cfg.InetBridge, err)
	}
	return nil
}

func ensureNamespace(cfg parsedConfig) error {
	log.Printf("[*] Netns + veth for INetSim…")
	hostHandle, err := netlink.NewHandle()
	if err != nil {
		return fmt.Errorf("host netlink handle: %w", err)
	}
	defer hostHandle.Close()

	nsHandle, ns, err := ensureNetns(cfg.Namespace)
	if err != nil {
		return err
	}
	defer nsHandle.Close()
	defer ns.Close()

	hostLink, err := hostHandle.LinkByName(cfg.VethHost)
	if err != nil {
		if !isLinkNotFound(err) {
			return fmt.Errorf("lookup %s: %w", cfg.VethHost, err)
		}
		veth := &netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name: cfg.VethHost,
			},
			PeerName: cfg.VethNamespace,
		}
		if err := hostHandle.LinkAdd(veth); err != nil && !errors.Is(err, syscall.EEXIST) {
			return fmt.Errorf("create veth: %w", err)
		}
		hostLink, err = hostHandle.LinkByName(cfg.VethHost)
		if err != nil {
			return fmt.Errorf("lookup veth host: %w", err)
		}
	}

	if _, err := nsHandle.LinkByName(cfg.VethNamespace); err != nil {
		if !isLinkNotFound(err) {
			return fmt.Errorf("lookup ns peer: %w", err)
		}
		peerLink, err := hostHandle.LinkByName(cfg.VethNamespace)
		if err != nil {
			return fmt.Errorf("peer link %s: %w", cfg.VethNamespace, err)
		}
		if err := hostHandle.LinkSetNsFd(peerLink, int(ns)); err != nil && !errors.Is(err, syscall.EEXIST) {
			return fmt.Errorf("move %s to ns: %w", cfg.VethNamespace, err)
		}
	}

	if err := hostHandle.LinkSetDown(hostLink); err != nil && !errors.Is(err, syscall.EOPNOTSUPP) {
		return fmt.Errorf("set %s down: %w", cfg.VethHost, err)
	}
	brLink, err := hostHandle.LinkByName(cfg.InetBridge)
	if err != nil {
		return fmt.Errorf("lookup bridge %s: %w", cfg.InetBridge, err)
	}
	if err := hostHandle.LinkSetMaster(hostLink, brLink); err != nil && !errors.Is(err, syscall.EEXIST) && !errors.Is(err, syscall.EBUSY) {
		return fmt.Errorf("enslave %s to %s: %w", cfg.VethHost, cfg.InetBridge, err)
	}
	if err := hostHandle.LinkSetUp(hostLink); err != nil {
		return fmt.Errorf("bring %s up: %w", cfg.VethHost, err)
	}

	if err := configureNamespaceLinks(nsHandle, cfg); err != nil {
		return err
	}
	return nil
}

func ensureNetns(name string) (*netlink.Handle, netns.NsHandle, error) {
	ns, err := netns.GetFromName(name)
	if err != nil {
		if !errors.Is(err, syscall.ENOENT) {
			return nil, 0, fmt.Errorf("get netns %s: %w", name, err)
		}
		if ns, err = netns.NewNamed(name); err != nil {
			return nil, 0, fmt.Errorf("create netns %s: %w", name, err)
		}
	}
	handle, err := netlink.NewHandleAt(ns)
	if err != nil {
		_ = ns.Close()
		return nil, 0, fmt.Errorf("handle for ns %s: %w", name, err)
	}
	return handle, ns, nil
}

func configureNamespaceLinks(nsHandle *netlink.Handle, cfg parsedConfig) error {
	if lo, err := nsHandle.LinkByName("lo"); err == nil {
		if err := nsHandle.LinkSetUp(lo); err != nil {
			return fmt.Errorf("bring lo up: %w", err)
		}
	}
	nsVeth, err := nsHandle.LinkByName(cfg.VethNamespace)
	if err != nil {
		return fmt.Errorf("ns veth %s: %w", cfg.VethNamespace, err)
	}
	if err := nsHandle.LinkSetUp(nsVeth); err != nil {
		return fmt.Errorf("bring %s up: %w", cfg.VethNamespace, err)
	}
	if err := ensureNamespaceAddress(nsHandle, nsVeth, cfg.inetNamespaceAddr); err != nil {
		return err
	}
	if err := nsHandle.RouteReplace(&netlink.Route{
		LinkIndex: nsVeth.Attrs().Index,
		Gw:        cfg.inetGateway.IP,
	}); err != nil {
		return fmt.Errorf("default route via %s: %w", cfg.inetGateway.IP, err)
	}
	return nil
}

func ensureNamespaceAddress(handle *netlink.Handle, link netlink.Link, addr *netlink.Addr) error {
	existing, err := handle.AddrList(link, unix.AF_INET)
	if err != nil {
		return fmt.Errorf("list addrs: %w", err)
	}
	for _, a := range existing {
		if a.IP.Equal(addr.IP) && bytesEqualMask(a.Mask, addr.Mask) {
			return nil
		}
	}
	if err := handle.AddrReplace(link, addr); err != nil {
		return fmt.Errorf("addr replace: %w", err)
	}
	return nil
}

func ensureAddress(link netlink.Link, addr *netlink.Addr) error {
	existing, err := netlink.AddrList(link, unix.AF_INET)
	if err != nil {
		return fmt.Errorf("list addresses: %w", err)
	}
	for _, a := range existing {
		if a.IP.Equal(addr.IP) && bytesEqualMask(a.Mask, addr.Mask) {
			return nil
		}
	}
	if err := netlink.AddrAdd(link, addr); err != nil && !errors.Is(err, syscall.EEXIST) {
		return fmt.Errorf("add %s to %s: %w", addr, link.Attrs().Name, err)
	}
	return nil
}

func configureSysctls(ctx context.Context) error {
	log.Printf("[*] Sysctls…")
	if err := writeSysctl("/proc/sys/net/ipv4/ip_forward", "1"); err != nil {
		return err
	}
	if err := writeSysctl("/proc/sys/net/ipv4/conf/all/rp_filter", "2"); err != nil {
		return err
	}
	log.Printf("[*] Enabling bridge-nf")
	if err := runCommand(ctx, "modprobe", "br_netfilter"); err != nil {
		return fmt.Errorf("modprobe br_netfilter: %w", err)
	}
	if err := writeSysctl("/proc/sys/net/bridge/bridge-nf-call-iptables", "0"); err != nil {
		return err
	}
	if err := writeSysctl("/proc/sys/net/bridge/bridge-nf-call-ip6tables", "0"); err != nil {
		return err
	}
	return nil
}

func writeSysctl(path, value string) error {
	return os.WriteFile(path, []byte(value), 0o644)
}

func programNftables(ctx context.Context, cfg parsedConfig) error {
	data, err := buildNftTemplateData(cfg)
	if err != nil {
		return err
	}

	log.Printf("[*] nftables (lab_nat / lab_flt)…")
	if _, err := commandSucceeds(ctx, "nft", "delete", "table", "ip", "lab_nat"); err != nil {
		return fmt.Errorf("nft delete table ip lab_nat: %w", err)
	}
	if _, err := commandSucceeds(ctx, "nft", "delete", "table", "inet", "lab_flt"); err != nil {
		return fmt.Errorf("nft delete table inet lab_flt: %w", err)
	}

	funcs := template.FuncMap{
		"join": strings.Join,
	}

	templateName := "lab_net"

	// Uses embedded 'lab_net' template
	tmpl, err := template.New(templateName).Funcs(funcs).Parse(labNetRules)
	if err != nil {
		return fmt.Errorf("parse nft template %s: %w", templateName, err)
	}
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		return fmt.Errorf("render nft template %s: %w", templateName, err)
	}
	if err := runCommandWithInput(ctx, "nft", rendered.Bytes(), "-f", "-"); err != nil {
		return fmt.Errorf("nft -f - (%s): %w", templateName, err)
	}
	return nil
}

func writeConfigFile(cfg Config) error {
	if err := os.MkdirAll(ConfigDir, 0o755); err != nil {
		return fmt.Errorf("make config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmpPath := filepath.Join(ConfigDir, "networking.json.tmp")
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	finalPath := filepath.Join(ConfigDir, "networking.json")
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}

func isLinkNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) {
		return true
	}
	var notFound netlink.LinkNotFoundError
	return errors.As(err, &notFound)
}

func bytesEqualMask(a, b net.IPMask) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runCommandWithInput(ctx context.Context, name string, input []byte, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = bytes.NewReader(input)
	return cmd.Run()
}

func outputCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func commandSucceeds(ctx context.Context, name string, args ...string) (bool, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	return false, err
}
