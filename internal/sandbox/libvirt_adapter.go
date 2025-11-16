package sandbox

import (
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cochaviz/mime/internal/artifacts"

	"github.com/google/uuid"
	libvirt "libvirt.org/go/libvirt"
)

var _ SandboxDriver = &LibvirtDriver{}

type LibvirtDriver struct {
	ConnectionURI string
	Logger        *slog.Logger
	BaseDir       string
}

//go:embed default_domain.xml
var defaultDomain string

func NewLibvirtDriver() *LibvirtDriver {
	return &LibvirtDriver{}
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

	setupLetter := resolveDeviceLetter(refSpec.DomainProfile.SetupLetter, "b")
	sampleFallbackLetter := "b"
	if strings.TrimSpace(spec.SetupDir) != "" {
		sampleFallbackLetter = nextDeviceLetter(setupLetter)
	}
	sampleLetter := resolveDeviceLetter(refSpec.DomainProfile.SampleLetter, sampleFallbackLetter)

	if strings.TrimSpace(spec.SetupDir) != "" {
		setupImage, err := prepareSandboxDisk(runDir, spec.SetupDir, "setup", sanitizeVolumeLabel(domainName, "setup"), true)
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
	}

	metadata := map[string]any{
		"driver":      "libvirt",
		"domain_name": domainName,
		"image_id":    spec.SandboxImage.ID,
	}

	logger.Info("sandbox workspace prepared")

	return SandboxLease{
		ID:            leaseID,
		Specification: spec,
		SandboxState:  SandboxPending,
		RunDir:        runDir,
		RuntimeConfig: runtimeConfig,
		Metadata:      metadata,
	}, nil
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

	if err := d.configureGuestMounts(domain, &lease, logger); err != nil {
		return lease, err
	}

	lease.SandboxState = SandboxRunning
	lease.StartTime = time.Now().UTC()
	return lease, nil
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

	logger := d.logger().With(
		"domain", domainName,
		"lease_id", lease.ID,
	)

	if connectionURI != "" && domainName != "" {
		conn, connErr := libvirt.NewConnect(connectionURI)
		if connErr == nil {
			defer conn.Close()

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
		} else if !force {
			return fmt.Errorf("open libvirt connection %s: %w", connectionURI, connErr)
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
