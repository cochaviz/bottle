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

	baseAbs, overlayAbs, err := createDiskOverlay(baseImagePath, filepath.Join(runDir, "disk-overlay.qcow2"))
	if err != nil {
		return SandboxLease{}, fmt.Errorf("prepare overlay disk: %w", err)
	}
	logger.Debug("created overlay disk", "base_image", baseAbs, "overlay", overlayAbs)

	domainData, err := buildDomainTemplateData(domainName, overlayAbs, refSpec)
	if err != nil {
		return SandboxLease{}, fmt.Errorf("derive domain template data: %w", err)
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
		StartTime:     time.Now().UTC(),
		Specification: spec,
		SandboxState:  SandboxPending,
		RunDir:        runDir,
		RuntimeConfig: runtimeConfig,
		Metadata:      metadata,
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

func (d *LibvirtDriver) Destroy(lease SandboxLease, force bool) error {
	// Implementation details
	return nil
}

func (d *LibvirtDriver) CollectMetrics(lease SandboxLease) (LeaseMetrics, error) {
	// Implementation details
	return LeaseMetrics{}, nil
}
