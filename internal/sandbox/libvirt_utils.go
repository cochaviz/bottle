package sandbox

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
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

func createDiskOverlay(baseImagePath, overlayPath string) (string, string, error) {
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

	cmd := exec.Command(qemuImg, "create", "-f", "qcow2", "-b", baseAbs, overlayAbs)
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
		VirtArch:      strings.TrimSpace(domainProfile.Arch),
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
