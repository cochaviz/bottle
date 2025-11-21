package sandbox

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"text/template"

	libvirt "libvirt.org/go/libvirt"
)

type domainDiskAttachment struct {
	File   string
	Target string
}

type domainTemplateData struct {
	Name          string
	RAM           int
	VCPUs         int
	VirtArch      string
	Machine       *string
	KernelPath    *string
	InitrdPath    *string
	KernelCmdline *string
	Overlay       string
	DiskTarget    string
	DiskBus       string
	SetupDisk     *domainDiskAttachment
	SampleDisk    *domainDiskAttachment
	ExtraDisks    []domainDiskAttachment
	CDBus         string
	Network       string
	NetworkModel  string
	NetworkMAC    string
}

func (d *LibvirtDriver) logger() *slog.Logger {
	if d != nil && d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}

func ensureRunDirectory(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("run directory is empty")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve run directory %q: %w", dir, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("create run directory %q: %w", abs, err)
	}
	return abs, nil
}

func createDiskOverlay(baseImagePath, overlayPath, backingFormat string) (string, string, error) {
	if baseImagePath == "" {
		return "", "", errors.New("base image path is empty")
	}
	if overlayPath == "" {
		return "", "", errors.New("overlay path is empty")
	}

	baseAbs, err := filepath.Abs(baseImagePath)
	if err != nil {
		return "", "", fmt.Errorf("resolve base image path %q: %w", baseImagePath, err)
	}
	if _, err := os.Stat(baseAbs); err != nil {
		return "", "", fmt.Errorf("stat base image %q: %w", baseAbs, err)
	}

	overlayAbs, err := filepath.Abs(overlayPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve overlay path %q: %w", overlayPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(overlayAbs), 0o755); err != nil {
		return "", "", fmt.Errorf("create overlay directory for %q: %w", overlayAbs, err)
	}
	if err := os.Remove(overlayAbs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("remove existing overlay %q: %w", overlayAbs, err)
	}

	qemuImg, err := exec.LookPath("qemu-img")
	if err != nil {
		return "", "", fmt.Errorf("qemu-img not found in PATH: %w", err)
	}

	// I think we might be able to do this without the backingFormat, but I'm not sure
	args := []string{qemuImg, "create", "-f", "qcow2", "-b", baseAbs}
	if backingFormat != "" {
		args = append(args, "-F", backingFormat)
	}
	args = append(args, overlayAbs)

	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("create overlay with qemu-img: %w (output: %s)", err, strings.TrimSpace(string(output)))
	}

	return baseAbs, overlayAbs, nil
}

func renderDomainXML(templateSrc string, data domainTemplateData) ([]byte, error) {
	if templateSrc == "" {
		return nil, errors.New("domain template source is empty")
	}

	tmpl, err := template.New("domain").Parse(templateSrc)
	if err != nil {
		return nil, fmt.Errorf("parse domain template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute domain template: %w", err)
	}
	return buf.Bytes(), nil
}

func buildDomainTemplateData(name, overlayPath string, spec SandboxSpecification) (domainTemplateData, error) {
	if name == "" {
		return domainTemplateData{}, errors.New("domain name is required")
	}
	if overlayPath == "" {
		return domainTemplateData{}, errors.New("overlay path is required")
	}

	domainProfile := spec.DomainProfile
	runProfile := spec.RunProfile

	ram := runProfile.RAMMB
	if ram == 0 {
		ram = domainProfile.RAMMB
	}
	if ram == 0 {
		return domainTemplateData{}, errors.New("ram value is not set in the specification")
	}

	vcpus := runProfile.VCPUs
	if vcpus == 0 {
		vcpus = domainProfile.VCPUs
	}
	if vcpus == 0 {
		return domainTemplateData{}, errors.New("vcpus value is not set in the specification")
	}

	if runProfile.BootMethod == BootMethodKernel {
		if runProfile.KernelPath == nil {
			return domainTemplateData{}, errors.New("kernel boot requested but KernelPath is not set")
		}
		if runProfile.InitrdPath == nil {
			return domainTemplateData{}, errors.New("kernel boot requested but InitrdPath is not set")
		}
	}

	diskTarget := domainProfile.DiskTarget
	if diskTarget == "" {
		diskTarget = "vda"
	}

	diskBus := domainProfile.DiskBus
	if diskBus == "" {
		diskBus = "virtio"
	}

	networkModel := domainProfile.NetworkModel
	if networkModel == "" {
		networkModel = "virtio"
	}

	networkName := runProfile.NetworkName
	if networkName == "" {
		networkName = "default"
	}

	cdBus := domainProfile.CDBus
	if cdBus == "" {
		cdBus = "sata"
	}

	return domainTemplateData{
		Name:          name,
		RAM:           ram,
		VCPUs:         vcpus,
		VirtArch:      strings.TrimSpace(domainProfile.Arch.String()),
		Machine:       domainProfile.Machine,
		KernelPath:    runProfile.KernelPath,
		InitrdPath:    runProfile.InitrdPath,
		KernelCmdline: runProfile.KernelCmdline,
		Overlay:       overlayPath,
		DiskTarget:    diskTarget,
		DiskBus:       diskBus,
		CDBus:         cdBus,
		Network:       networkName,
		NetworkModel:  networkModel,
	}, nil
}

func runtimeString(config map[string]any, key string) (string, error) {
	if config == nil {
		return "", fmt.Errorf("runtime config missing %s", key)
	}
	value, ok := config[key]
	if !ok {
		return "", fmt.Errorf("runtime config missing %s", key)
	}

	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("runtime config %s must be a string", key)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("runtime config %s is empty", key)
	}
	return text, nil
}

func isLibvirtNotFound(err error) bool {
	return isLibvirtError(err, libvirt.ERR_NO_DOMAIN)
}

func isLibvirtError(err error, codes ...libvirt.ErrorNumber) bool {
	if err == nil {
		return false
	}
	var libvirtErr libvirt.Error
	if !errors.As(err, &libvirtErr) {
		return false
	}

	return slices.Contains(codes, libvirtErr.Code)
}

func detectDiskFormat(image SandboxImage, basePath string) string {
	if format := normalizeDiskFormat(image.ImageArtifact.ContentType); format != "" {
		return format
	}
	if image.ImageArtifact.Metadata != nil {
		if value, ok := image.ImageArtifact.Metadata["disk_format"].(string); ok {
			if format := normalizeDiskFormat(value); format != "" {
				return format
			}
		}
	}

	switch strings.ToLower(filepath.Ext(basePath)) {
	case ".qcow2":
		return "qcow2"
	case ".raw":
		return "raw"
	default:
		return ""
	}
}

func normalizeDiskFormat(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "qcow", "qcow2":
		return "qcow2"
	case "raw":
		return "raw"
	default:
		return ""
	}
}

type domainInterfaceTarget struct {
	MAC struct {
		Address string `xml:"address,attr"`
	} `xml:"mac"`
	Target struct {
		Dev string `xml:"dev,attr"`
	} `xml:"target"`
}

type domainDevices struct {
	Interfaces []domainInterfaceTarget `xml:"interface"`
}

type domainXMLDesc struct {
	Devices domainDevices `xml:"devices"`
}

func hostInterfaceForMAC(domain *libvirt.Domain, mac string) (string, error) {
	if domain == nil {
		return "", errors.New("domain handle is nil")
	}
	xmlDesc, err := domain.GetXMLDesc(0)
	if err != nil {
		return "", fmt.Errorf("get domain XML: %w", err)
	}
	var desc domainXMLDesc
	if err := xml.Unmarshal([]byte(xmlDesc), &desc); err != nil {
		return "", fmt.Errorf("parse domain XML: %w", err)
	}
	targetMAC := strings.ToLower(strings.TrimSpace(mac))
	for _, iface := range desc.Devices.Interfaces {
		if strings.ToLower(strings.TrimSpace(iface.MAC.Address)) != targetMAC {
			continue
		}
		dev := strings.TrimSpace(iface.Target.Dev)
		if dev == "" {
			return "", fmt.Errorf("interface target missing for mac %s", mac)
		}
		return dev, nil
	}
	return "", fmt.Errorf("interface for mac %s not found in domain XML", mac)
}
