package sandbox

import (
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cochaviz/bottle/internal/artifacts"

	"github.com/google/uuid"
	libvirt "libvirt.org/go/libvirt"
)

var _ SandboxDriver = NewLibvirtDriver()

type LibvirtDriver struct {
	ConnectionURI string
	Logger        *slog.Logger
	BaseDir       string

	networkFactory networkHandleFactory
}

//go:embed default_domain.xml
var defaultDomain string

func NewLibvirtDriver() *LibvirtDriver {
	return &LibvirtDriver{}
}

type networkHandleFactory func(uri, networkName string) (libvirtNetwork, func(), error)

func (d *LibvirtDriver) networkHandle(networkName string) (libvirtNetwork, func(), error) {
	factory := d.networkFactory
	if factory == nil {
		factory = defaultNetworkHandleFactory
	}
	handle, cleanup, err := factory(d.ConnectionURI, networkName)
	if err != nil {
		return nil, nil, err
	}
	if cleanup == nil {
		cleanup = func() {}
	}
	return handle, cleanup, nil
}

func defaultNetworkHandleFactory(uri, networkName string) (libvirtNetwork, func(), error) {
	conn, err := libvirt.NewConnect(uri)
	if err != nil {
		return nil, nil, fmt.Errorf("open libvirt connection %s: %w", uri, err)
	}
	network, err := conn.LookupNetworkByName(networkName)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("lookup network %s: %w", networkName, err)
	}
	cleanup := func() {
		network.Free()
		conn.Close()
	}
	return newLibvirtNetworkDriver(network), cleanup, nil
}

func (d *LibvirtDriver) Acquire(spec SandboxLeaseSpecification) (SandboxLease, error) {
	if spec.OverrideSpecification != nil {
		return SandboxLease{}, fmt.Errorf("override specification is currently not supported, please build a new image using the customized specification")
	}
	if d == nil {
		return SandboxLease{}, fmt.Errorf("libvirt driver is not configured")
	}
	if d.BaseDir == "" {
		return SandboxLease{}, fmt.Errorf("libvirt driver BaseDir is not configured")
	}
	if d.ConnectionURI == "" {
		return SandboxLease{}, fmt.Errorf("libvirt driver ConnectionURI is not configured")
	}
	if spec.SandboxImage.ImageArtifact.URI == "" {
		return SandboxLease{}, fmt.Errorf("sandbox image artifact URI is required")
	}

	leaseID := uuid.New().String()
	if spec.DomainName != "" {
		leaseID = spec.DomainName
	}

	runDir, err := ensureRunDirectory(filepath.Join(d.BaseDir, leaseID))
	if err != nil {
		return SandboxLease{}, err
	}

	refSpec := spec.SandboxImage.ReferenceSpecification
	domainName := spec.DomainName
	if strings.TrimSpace(domainName) == "" {
		domainName = leaseID
		if prefix := strings.TrimSpace(refSpec.RunProfile.NamePrefix); prefix != "" {
			domainName = fmt.Sprintf("%s-%s", prefix, leaseID)
		}
	}

	logger := d.logger().With(
		"lease_id", leaseID,
		"domain", domainName,
		"run_dir", runDir,
	)
	logger.Info("preparing sandbox workspace")

	baseImagePath, err := artifacts.PathFromURI(spec.SandboxImage.ImageArtifact.URI)
	if err != nil {
		return SandboxLease{}, fmt.Errorf("resolve sandbox image artifact path: %w", err)
	}
	baseImagePath = filepath.Clean(baseImagePath)

	backingFormat := detectDiskFormat(spec.SandboxImage, baseImagePath)

	baseAbs, overlayAbs, err := createDiskOverlay(baseImagePath, filepath.Join(runDir, "disk-overlay.qcow2"), backingFormat)
	if err != nil {
		return SandboxLease{}, fmt.Errorf("prepare overlay disk: %w", err)
	}
	logger.Debug("created overlay disk", "base_image", baseAbs, "overlay", overlayAbs)

	domainData, err := buildDomainTemplateData(domainName, overlayAbs, refSpec)
	if err != nil {
		return SandboxLease{}, fmt.Errorf("derive domain template data: %w", err)
	}
	networkName := strings.TrimSpace(domainData.Network)
	if networkName == "" {
		return SandboxLease{}, fmt.Errorf("sandbox network is not configured")
	}
	macAddress := generateSandboxMAC(leaseID)
	domainData.NetworkMAC = macAddress

	setupEntries, err := computeSetupFileEntries(refSpec.SetupFiles)
	if err != nil {
		return SandboxLease{}, fmt.Errorf("prepare setup files: %w", err)
	}

	setupLetter := resolveDeviceLetter(refSpec.DomainProfile.SetupLetter, "b")
	sampleFallbackLetter := "b"
	if len(setupEntries) > 0 {
		sampleFallbackLetter = nextDeviceLetter(setupLetter)
	}
	sampleLetter := resolveDeviceLetter(refSpec.DomainProfile.SampleLetter, sampleFallbackLetter)

	if len(setupEntries) > 0 {
		setupStagingDir, err := stageSetupFiles(runDir, setupEntries)
		if err != nil {
			return SandboxLease{}, fmt.Errorf("stage setup files: %w", err)
		}
		defer os.RemoveAll(setupStagingDir)

		setupImage, err := prepareSandboxDisk(runDir, setupStagingDir, "setup", sanitizeVolumeLabel(domainName, "setup"), true)
		if err != nil {
			return SandboxLease{}, fmt.Errorf("prepare setup disk: %w", err)
		}
		domainData.SetupDisk = &domainDiskAttachment{
			File:   setupImage,
			Target: cdDeviceTarget(refSpec.DomainProfile.CDPrefix, setupLetter),
		}
		logger.Debug("prepared setup disk", "path", setupImage)
	}

	if strings.TrimSpace(spec.SampleDir) != "" {
		sampleImage, err := prepareSandboxDisk(runDir, spec.SampleDir, "sample", sanitizeVolumeLabel(domainName, "sample"), false)
		if err != nil {
			return SandboxLease{}, fmt.Errorf("prepare sample disk: %w", err)
		}
		domainData.SampleDisk = &domainDiskAttachment{
			File:   sampleImage,
			Target: cdDeviceTarget(refSpec.DomainProfile.CDPrefix, sampleLetter),
		}
		logger.Debug("prepared sample disk", "path", sampleImage)
	}

	network, releaseNetwork, err := d.networkHandle(networkName)
	if err != nil {
		return SandboxLease{}, err
	}
	defer releaseNetwork()

	var reservedLease NetworkLease
	reservationActive := false
	success := false
	defer func() {
		if reservationActive && !success {
			if err := network.Release(reservedLease); err != nil {
				d.logger().Error("rollback DHCP reservation failed", "error", err, "network", networkName, "mac", macAddress, "ip", reservedLease.IP)
			}
		}
	}()

	reservedLease, err = network.Acquire(macAddress)
	if err != nil {
		return SandboxLease{}, fmt.Errorf("reserve DHCP lease: %w", err)
	}
	reservationActive = true

	domainXML, err := renderDomainXML(defaultDomain, domainData)
	if err != nil {
		return SandboxLease{}, fmt.Errorf("render domain definition: %w", err)
	}

	domainXMLPath := filepath.Join(runDir, "domain.xml")
	if err := os.WriteFile(domainXMLPath, domainXML, 0o644); err != nil {
		return SandboxLease{}, fmt.Errorf("write domain definition: %w", err)
	}
	logger.Debug("wrote domain definition", "path", domainXMLPath)

	runtimeConfig := map[string]any{
		"domain_name":    domainName,
		"domain_xml":     domainXMLPath,
		"overlay_path":   overlayAbs,
		"base_image":     baseAbs,
		"connection_uri": d.ConnectionURI,
		"network_name":   networkName,
		"dhcp_mac":       macAddress,
		"dhcp_ip":        reservedLease.IP.String(),
	}

	metadata := map[string]any{
		"driver":      "libvirt",
		"domain_name": domainName,
		"image_id":    spec.SandboxImage.ID,
		"vm_ip":       reservedLease.IP.String(),
		"vm_mac":      strings.ToLower(reservedLease.MAC),
	}

	logger.Info("sandbox workspace prepared")

	lease := SandboxLease{
		ID:            leaseID,
		Specification: spec,
		SandboxState:  SandboxPending,
		RunDir:        runDir,
		RuntimeConfig: runtimeConfig,
		Metadata:      metadata,
	}
	success = true
	return lease, nil
}

func (d *LibvirtDriver) Start(lease SandboxLease) (SandboxLease, error) {
	domainXMLPath, err := runtimeString(lease.RuntimeConfig, "domain_xml")
	if err != nil {
		return lease, err
	}
	connectionURI, err := runtimeString(lease.RuntimeConfig, "connection_uri")
	if err != nil {
		return lease, err
	}
	domainName, err := runtimeString(lease.RuntimeConfig, "domain_name")
	if err != nil {
		return lease, err
	}

	logger := d.logger().With(
		"domain", domainName,
		"lease_id", lease.ID,
	)

	xmlPayload, err := os.ReadFile(domainXMLPath)
	if err != nil {
		return lease, fmt.Errorf("read domain definition: %w", err)
	}

	conn, err := libvirt.NewConnect(connectionURI)
	if err != nil {
		return lease, fmt.Errorf("open libvirt connection %s: %w", connectionURI, err)
	}
	defer conn.Close()

	setupEntries, err := computeSetupFileEntries(lease.Specification.SandboxImage.ReferenceSpecification.SetupFiles)
	if err != nil {
		return lease, fmt.Errorf("resolve setup files: %w", err)
	}

	var domain *libvirt.Domain
	domain, err = conn.LookupDomainByName(domainName)
	if err != nil {
		if !isLibvirtNotFound(err) {
			return lease, fmt.Errorf("lookup domain %s: %w", domainName, err)
		}
		domain, err = conn.DomainDefineXML(string(xmlPayload))
		if err != nil {
			return lease, fmt.Errorf("define domain %s: %w", domainName, err)
		}
	}
	defer domain.Free()

	state, _, err := domain.GetState()
	if err != nil {
		return lease, fmt.Errorf("query domain state: %w", err)
	}

	if state != libvirt.DOMAIN_RUNNING && state != libvirt.DOMAIN_BLOCKED {
		if err := domain.Create(); err != nil {
			return lease, fmt.Errorf("start domain %s: %w", domainName, err)
		}
		logger.Info("sandbox domain started")
	} else {
		logger.Info("sandbox domain already running", "state", state)
	}

	setupMountPath, _, err := d.configureGuestMounts(domain, &lease, setupEntries, logger)
	if err != nil {
		return lease, err
	}

	if err := d.runSetupScripts(domain, setupEntries, setupMountPath, logger); err != nil {
		return lease, err
	}
	if err := d.attachVMInterfaceMetadata(domain, &lease); err != nil {
		logger.Warn("determine vm interface failed", "error", err)
	}

	lease.SandboxState = SandboxRunning
	lease.StartTime = time.Now().UTC()
	return lease, nil
}

func (d *LibvirtDriver) runSetupScripts(domain *libvirt.Domain, entries []setupFileEntry, setupMountPath string, logger *slog.Logger) error {
	if len(entries) == 0 {
		return nil
	}

	if strings.TrimSpace(setupMountPath) == "" {
		return fmt.Errorf("setup mount path not detected")
	}

	for _, entry := range entries {
		guestPath := filepath.Join(setupMountPath, entry.FileName)
		if _, err := runGuestCommand(domain, "/bin/bash", []string{guestPath}, guestMountTimeout); err != nil {
			return fmt.Errorf("execute setup script %s: %w", entry.FileName, err)
		}
		logger.Info("setup script executed", "script", entry.FileName)
	}

	return nil
}

func (d *LibvirtDriver) attachVMInterfaceMetadata(domain *libvirt.Domain, lease *SandboxLease) error {
	if lease == nil {
		return errors.New("sandbox lease is nil")
	}
	if domain == nil {
		return errors.New("domain handle is nil")
	}
	mac, err := runtimeString(lease.RuntimeConfig, "dhcp_mac")
	if err != nil {
		return err
	}
	iface, err := hostInterfaceForMAC(domain, mac)
	if err != nil {
		return err
	}
	if lease.Metadata == nil {
		lease.Metadata = map[string]any{}
	}
	lease.Metadata["vm_interface"] = iface
	return nil
}

func (d *LibvirtDriver) Execute(lease SandboxLease, command SandboxCommand) (SandboxCommandResult, error) {
	domainName, err := runtimeString(lease.RuntimeConfig, "domain_name")
	if err != nil {
		return SandboxCommandResult{}, err
	}
	connectionURI, err := runtimeString(lease.RuntimeConfig, "connection_uri")
	if err != nil {
		return SandboxCommandResult{}, err
	}
	if strings.TrimSpace(command.Path) == "" {
		return SandboxCommandResult{}, fmt.Errorf("command path is required")
	}

	logger := d.logger().With(
		"domain", domainName,
		"lease_id", lease.ID,
	)

	conn, err := libvirt.NewConnect(connectionURI)
	if err != nil {
		return SandboxCommandResult{}, fmt.Errorf("open libvirt connection %s: %w", connectionURI, err)
	}
	defer conn.Close()

	domain, err := conn.LookupDomainByName(domainName)
	if err != nil {
		return SandboxCommandResult{}, fmt.Errorf("lookup domain %s: %w", domainName, err)
	}
	defer domain.Free()

	if err := waitForGuestAgent(domain, 5*time.Second, 24); err != nil {
		return SandboxCommandResult{}, err
	}

	timeout := command.Timeout
	if timeout <= 0 {
		timeout = guestMountTimeout
	}

	result, err := runGuestCommand(domain, command.Path, command.Args, timeout)
	if err != nil {
		return SandboxCommandResult{
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
			ExitCode: result.ExitCode,
		}, err
	}

	logger.Debug("guest command executed", "path", command.Path, "exit_code", result.ExitCode)

	return SandboxCommandResult{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
	}, nil
}

func (d *LibvirtDriver) Pause(lease SandboxLease, reason string) (SandboxLease, error) {
	// Implementation details
	return SandboxLease{}, fmt.Errorf("pause not implemented")
}

func (d *LibvirtDriver) Resume(lease SandboxLease) (SandboxLease, error) {
	// Implementation details
	return SandboxLease{}, fmt.Errorf("resume not implemented")
}

func (d *LibvirtDriver) Release(lease SandboxLease, force bool) error {
	connectionURI, err := runtimeString(lease.RuntimeConfig, "connection_uri")
	if err != nil {
		// If runtime configuration is missing, we can only clean up the run dir.
		connectionURI = ""
	}
	domainName, err := runtimeString(lease.RuntimeConfig, "domain_name")
	if err != nil {
		domainName = ""
	}
	networkName, err := runtimeString(lease.RuntimeConfig, "network_name")
	if err != nil {
		networkName = ""
	}
	dhcpMAC, err := runtimeString(lease.RuntimeConfig, "dhcp_mac")
	if err != nil {
		dhcpMAC = ""
	}
	dhcpIP, err := runtimeString(lease.RuntimeConfig, "dhcp_ip")
	if err != nil {
		dhcpIP = ""
	}

	logger := d.logger().With(
		"domain", domainName,
		"lease_id", lease.ID,
	)

	var conn *libvirt.Connect
	if connectionURI != "" {
		c, connErr := libvirt.NewConnect(connectionURI)
		if connErr != nil {
			if !force {
				return fmt.Errorf("open libvirt connection %s: %w", connectionURI, connErr)
			}
			logger.Error("failed to open libvirt connection", "error", connErr, "uri", connectionURI)
		} else {
			conn = c
			defer conn.Close()
		}
	}

	if conn != nil && domainName != "" {
		domain, lookupErr := conn.LookupDomainByName(domainName)
		if lookupErr == nil {
			defer domain.Free()

			if err := domain.Destroy(); err != nil && !isLibvirtError(err, libvirt.ERR_OPERATION_INVALID, libvirt.ERR_NO_DOMAIN) {
				logger.Error("failed to destroy domain", "error", err)
				if !force {
					return fmt.Errorf("destroy domain %s: %w", domainName, err)
				}
			}
			if err := domain.Undefine(); err != nil && !isLibvirtError(err, libvirt.ERR_NO_DOMAIN) {
				logger.Error("failed to undefine domain", "error", err)
				if !force {
					return fmt.Errorf("undefine domain %s: %w", domainName, err)
				}
			}
		} else if !isLibvirtNotFound(lookupErr) && !force {
			return fmt.Errorf("lookup domain %s: %w", domainName, lookupErr)
		}
	}

	if conn != nil && networkName != "" && (dhcpMAC != "" || dhcpIP != "") {
		if network, lookupErr := conn.LookupNetworkByName(networkName); lookupErr == nil {
			lease := NetworkLease{
				MAC: strings.ToLower(strings.TrimSpace(dhcpMAC)),
			}
			if ip := net.ParseIP(strings.TrimSpace(dhcpIP)); ip != nil {
				lease.IP = ip
			}
			if err := newLibvirtNetworkDriver(network).Release(lease); err != nil && !force {
				return err
			}
			network.Free()
		} else if !isLibvirtNotFound(lookupErr) && !force {
			return fmt.Errorf("lookup network %s: %w", networkName, lookupErr)
		}
	}

	if lease.RunDir != "" {
		if err := os.RemoveAll(lease.RunDir); err != nil {
			return fmt.Errorf("cleanup run directory %s: %w", lease.RunDir, err)
		}
	}
	logger.Info("sandbox resources destroyed")
	return nil
}

func (d *LibvirtDriver) CollectMetrics(lease SandboxLease) (SandboxMetrics, error) {
	connectionURI, err := runtimeString(lease.RuntimeConfig, "connection_uri")
	if err != nil {
		return SandboxMetrics{}, err
	}
	domainName, err := runtimeString(lease.RuntimeConfig, "domain_name")
	if err != nil {
		return SandboxMetrics{}, err
	}

	conn, err := libvirt.NewConnect(connectionURI)
	if err != nil {
		return SandboxMetrics{}, fmt.Errorf("open libvirt connection %s: %w", connectionURI, err)
	}
	defer conn.Close()

	domain, err := conn.LookupDomainByName(domainName)
	if err != nil {
		return SandboxMetrics{}, fmt.Errorf("lookup domain %s: %w", domainName, err)
	}
	defer domain.Free()

	info, err := domain.GetInfo()
	if err != nil {
		return SandboxMetrics{}, fmt.Errorf("get domain info: %w", err)
	}

	state := info.State
	if state != libvirt.DOMAIN_RUNNING && state != libvirt.DOMAIN_BLOCKED {
		return SandboxMetrics{}, SandboxInterruptedError{Reason: fmt.Sprintf("domain %s not running (state=%d)", domainName, state)}
	}

	additional := map[string]any{
		"state": state,
	}

	return SandboxMetrics{
		CPUPercent:        0,
		MemoryBytes:       uint64(info.Memory) * 1024,
		GuestHeartbeatOK:  false,
		AdditionalMetrics: additional,
	}, nil
}
