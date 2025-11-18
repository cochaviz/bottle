package repositories

import (
	"errors"
	"fmt"
	"strings"

	"cochaviz/bottle/internal/artifacts"
	"cochaviz/bottle/internal/build"
	"cochaviz/bottle/internal/sandbox"
)

const (
	debianMirror     = "deb.debian.org"
	debianMirrorPath = "/debian"
	defaultRelease   = "bookworm"
	defaultVersion   = "2024.01"
	defaultDiskSize  = 4
	defaultNetwork   = "build"
)

// EmbeddedSpecificationRepository contains built-in sandbox specifications.
type EmbeddedSpecificationRepository struct {
	history map[string][]build.BuildSpecification
	order   []string
}

// NewEmbeddedSpecificationRepository constructs a repository pre-populated with embedded specs.
func NewEmbeddedSpecificationRepository() *EmbeddedSpecificationRepository {
	repo := &EmbeddedSpecificationRepository{
		history: make(map[string][]build.BuildSpecification),
	}

	for _, spec := range defaultSpecs() {
		repo.append(spec)
	}

	return repo
}

// Get returns the latest specification for the provided id.
func (r *EmbeddedSpecificationRepository) Get(specID string) (build.BuildSpecification, error) {
	versions, ok := r.history[specID]
	if !ok || len(versions) == 0 {
		return build.BuildSpecification{}, errors.New("specification not found")
	}
	return versions[len(versions)-1], nil
}

// Save adds a new version for the provided specification.
func (r *EmbeddedSpecificationRepository) Save(spec build.BuildSpecification) (build.BuildSpecification, error) {
	r.append(spec)
	return spec, nil
}

// ListVersions lists all known versions for a specification id.
func (r *EmbeddedSpecificationRepository) ListVersions(specID string) ([]build.BuildSpecification, error) {
	versions := r.history[specID]
	if len(versions) == 0 {
		return nil, nil
	}

	result := make([]build.BuildSpecification, len(versions))
	copy(result, versions)
	return result, nil
}

// ListAll returns the latest version for every specification.
func (r *EmbeddedSpecificationRepository) ListAll() ([]build.BuildSpecification, error) {
	if len(r.history) == 0 {
		return nil, nil
	}

	specs := make([]build.BuildSpecification, 0, len(r.order))
	for _, id := range r.order {
		if versions := r.history[id]; len(versions) > 0 {
			specs = append(specs, versions[len(versions)-1])
		}
	}
	return specs, nil
}

// FilterByArchitecture returns specs matching the requested architecture.
func (r *EmbeddedSpecificationRepository) FilterByArchitecture(architecture string) ([]build.BuildSpecification, error) {
	if architecture == "" {
		return r.ListAll()
	}

	all, err := r.ListAll()
	if err != nil {
		return nil, err
	}

	var matched []build.BuildSpecification
	for _, spec := range all {
		if strings.EqualFold(spec.SandboxSpecification.DomainProfile.Arch, architecture) {
			matched = append(matched, spec)
			continue
		}

		if spec.SandboxSpecification.Metadata != nil {
			if arch, ok := spec.SandboxSpecification.Metadata["arch"].(string); ok && strings.EqualFold(arch, architecture) {
				matched = append(matched, spec)
			}
		}
	}
	return matched, nil
}

func (r *EmbeddedSpecificationRepository) append(spec build.BuildSpecification) {
	if _, exists := r.history[spec.ID]; !exists {
		r.order = append(r.order, spec.ID)
	}
	r.history[spec.ID] = append(r.history[spec.ID], spec)
}

func defaultSpecs() []build.BuildSpecification {
	return []build.BuildSpecification{
		makeSpec(
			"debian-bookworm-amd64",
			"amd64",
			"ttyS0",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-amd64/current/images/netboot/debian-installer/amd64/linux",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-amd64/current/images/netboot/debian-installer/amd64/initrd.gz",
			sandbox.DomainProfile{
				Arch:         "x86_64",
				VCPUs:        2,
				RAMMB:        4096,
				DiskBus:      "virtio",
				DiskTarget:   "vda",
				CDBus:        "sata",
				CDPrefix:     "sd",
				NetworkModel: "virtio",
				ExtraArgs:    []string{"console=ttyS0,115200n8"},
			},
		),
		makeSpec(
			"debian-bookworm-i386",
			"i386",
			"ttyS0",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-i386/current/images/netboot/debian-installer/i386/linux",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-i386/current/images/netboot/debian-installer/i386/initrd.gz",
			sandbox.DomainProfile{
				Arch:         "i686",
				Machine:      strPtr("pc"),
				CPUModel:     strPtr("qemu32"),
				VCPUs:        1,
				RAMMB:        2048,
				DiskBus:      "virtio",
				DiskTarget:   "vda",
				CDBus:        "ide",
				CDPrefix:     "hd",
				NetworkModel: "virtio",
				ExtraArgs:    []string{"console=ttyS0,115200n8"},
			},
		),
		makeSpec(
			"debian-bookworm-arm64",
			"arm64",
			"ttyAMA0",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-arm64/current/images/netboot/debian-installer/arm64/linux",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-arm64/current/images/netboot/debian-installer/arm64/initrd.gz",
			sandbox.DomainProfile{
				Arch:         "aarch64",
				Machine:      strPtr("virt"),
				CPUModel:     strPtr("cortex-a72"),
				VCPUs:        2,
				RAMMB:        4096,
				DiskBus:      "virtio",
				DiskTarget:   "vda",
				CDBus:        "scsi",
				CDPrefix:     "sd",
				NetworkModel: "virtio",
				ExtraArgs:    []string{"console=ttyAMA0,115200"},
			},
		),
		makeSpec(
			"debian-bookworm-armhf",
			"armhf",
			"ttyAMA0",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-armhf/current/images/netboot/vmlinuz",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-armhf/current/images/netboot/initrd.gz",
			sandbox.DomainProfile{
				Arch:         "armv7l",
				Machine:      strPtr("virt"),
				CPUModel:     strPtr("cortex-a15"),
				VCPUs:        2,
				RAMMB:        3072,
				DiskBus:      "virtio",
				DiskTarget:   "vda",
				CDBus:        "scsi",
				CDPrefix:     "sd",
				NetworkModel: "virtio",
				ExtraArgs:    []string{"console=ttyAMA0,115200"},
			},
		),
		makeSpec(
			"debian-bookworm-ppc64el",
			"ppc64el",
			"hvc0",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-ppc64el/current/images/netboot/debian-installer/ppc64el/vmlinux",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-ppc64el/current/images/netboot/debian-installer/ppc64el/initrd.gz",
			sandbox.DomainProfile{
				Arch:         "ppc64le",
				Machine:      strPtr("pseries"),
				CPUModel:     strPtr("power9"),
				VCPUs:        2,
				RAMMB:        4096,
				DiskBus:      "virtio",
				DiskTarget:   "vda",
				CDBus:        "scsi",
				CDPrefix:     "sd",
				NetworkModel: "virtio",
				ExtraArgs:    []string{"console=hvc0"},
			},
		),
		makeSpec(
			"debian-bookworm-s390x",
			"s390x",
			"ttysclp0",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-s390x/current/images/netboot/debian-installer/s390x/linux",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-s390x/current/images/netboot/debian-installer/s390x/initrd.gz",
			sandbox.DomainProfile{
				Arch:         "s390x",
				Machine:      strPtr("s390-ccw-virtio"),
				CPUModel:     strPtr("z14"),
				VCPUs:        2,
				RAMMB:        4096,
				DiskBus:      "virtio",
				DiskTarget:   "vda",
				CDBus:        "virtio-scsi",
				CDPrefix:     "sd",
				NetworkModel: "virtio",
				ExtraArgs:    []string{"console=ttysclp0"},
			},
		),
		makeSpec(
			"debian-bookworm-mipsel",
			"mipsel",
			"ttyS0",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-mipsel/current/images/malta/netboot/vmlinuz-6.1.0-39-4kc-malta",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-mipsel/current/images/malta/netboot/initrd.gz",
			sandbox.DomainProfile{
				Arch:         "mipsel",
				Machine:      strPtr("malta"),
				CPUModel:     strPtr("24Kf"),
				VCPUs:        1,
				RAMMB:        2048,
				DiskBus:      "virtio",
				DiskTarget:   "vda",
				CDBus:        "ide",
				CDPrefix:     "hd",
				NetworkModel: "virtio",
				ExtraArgs:    []string{"console=ttyS0,115200"},
			},
		),
	}
}

func makeSpec(
	specID string,
	arch string,
	console string,
	kernelURL string,
	initrdURL string,
	domain sandbox.DomainProfile,
) build.BuildSpecification {
	domainCopy := domain
	if len(domain.ExtraArgs) > 0 {
		extra := make([]string, len(domain.ExtraArgs))
		copy(extra, domain.ExtraArgs)
		domainCopy.ExtraArgs = extra
	}

	return build.BuildSpecification{
		ID: specID,
		SandboxSpecification: sandbox.SandboxSpecification{
			ID:            specID,
			Version:       defaultVersion,
			OSRelease:     "debian-" + defaultRelease,
			DomainProfile: domainCopy,
			RunProfile:    runProfile(sandbox.BootMethodBIOS),
			NetworkLayout: networkLayout(),
			Metadata: map[string]any{
				"arch":        arch,
				"maintainer":  "embedded",
				"description": "Embedded Debian netinst profile for sandbox builds",
			},
			SetupFiles: defaultSetupFiles(),
		},
		InstallerAssets: map[string]string{
			"preseed_content":       preseedContent,
			"network_configuration": buildNetworkContent,
		},
		Profile: build.BuildProfile{
			Console:        console,
			KernelURL:      kernelURL,
			InitrdURL:      initrdURL,
			Release:        defaultRelease,
			DiskSizeGB:     defaultDiskSize,
			PreseedEnabled: true,
			MirrorHost:     debianMirror,
			MirrorPath:     debianMirrorPath,
			NetworkName:    defaultNetwork,
		},
	}
}

func strPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func runProfile(bootMethod sandbox.BootMethod) sandbox.RunProfile {
	return sandbox.RunProfile{
		RAMMB:       2048,
		VCPUs:       2,
		BootMethod:  bootMethod,
		NetworkName: "lab_net",
		NamePrefix:  "sandbox",
	}
}

func networkLayout() map[string]any {
	return map[string]any{
		"interfaces": []map[string]any{
			{
				"name":       "eth0",
				"model":      "virtio",
				"addressing": "dhcp",
			},
		},
	}
}

var (
	preseedContent      = embeddedPreseed
	buildNetworkContent = embeddedBuildNetwork
)

func defaultSetupFiles() []artifacts.Artifact {
	scriptPath, err := materializeSetupScript()
	if err != nil {
		panic(fmt.Sprintf("materialize setup script: %v", err))
	}
	return []artifacts.Artifact{
		{
			ID:   "bringup-dhcp",
			Kind: artifacts.TextArtifact,
			URI:  fmt.Sprintf("file://%s", scriptPath),
			Metadata: map[string]any{
				"filename": "bringup-dhcp.sh",
			},
		},
	}
}
