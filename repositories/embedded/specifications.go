package embedded

import (
	"errors"
	"strings"

	models "cochaviz/mime/models"
)

const (
	debianMirror     = "deb.debian.org"
	debianMirrorPath = "/debian"
	defaultRelease   = "bookworm"
	defaultVersion   = "2024.01"
	defaultDiskSize  = 4
	defaultNetwork   = "build"
)

const preseedContent = `d-i debian-installer/locale string en_US
d-i keyboard-configuration/xkb-keymap select us
d-i netcfg/choose_interface select auto
d-i netcfg/get_hostname string unassigned-hostname
d-i netcfg/get_domain string unassigned-domain
d-i netcfg/wireless_wep string
d-i mirror/country string manual
d-i mirror/http/hostname string http.us.debian.org
d-i mirror/http/directory string /debian
d-i mirror/http/proxy string
d-i passwd/make-user boolean false
d-i passwd/root-password password lab
d-i passwd/root-password-again password lab
d-i clock-setup/utc boolean true
d-i time/zone string US/Eastern
d-i clock-setup/ntp boolean true
d-i partman-auto/method string lvm
d-i partman-auto-lvm/guided_size string max
d-i partman-lvm/device_remove_lvm boolean true
d-i partman-md/device_remove_md boolean true
d-i partman-lvm/confirm boolean true
d-i partman-lvm/confirm_nooverwrite boolean true
d-i partman-auto/choose_recipe select atomic
d-i partman-partitioning/confirm_write_new_label boolean true
d-i partman/choose_partition select finish
d-i partman/confirm boolean true
d-i partman/confirm_nooverwrite boolean true
d-i partman-md/confirm boolean true
d-i partman-partitioning/confirm_write_new_label boolean true
d-i partman/choose_partition select finish
d-i partman/confirm boolean true
d-i partman/confirm_nooverwrite boolean true
d-i apt-setup/cdrom/set-first boolean false
d-i grub-installer/only_debian boolean true
d-i grub-installer/with_other_os boolean true
d-i grub-installer/bootdev  string default
d-i finish-install/reboot_in_progress note
d-i preseed/late_command \
        string apt-install qemu-guest-agent && in-target systemctl enable qemu-guest-agent.service
`

const buildNetworkContent = `<network>
  <name>build</name>
  <forward mode='nat'/>
  <ip address='192.168.252.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.252.2' end='192.168.252.254'/>
    </dhcp>
  </ip>
</network>
`

// EmbeddedSpecificationRepository contains built-in sandbox specifications.
type EmbeddedSpecificationRepository struct {
	history map[string][]models.SandboxSpecification
	order   []string
}

// NewEmbeddedSpecificationRepository constructs a repository pre-populated with embedded specs.
func NewEmbeddedSpecificationRepository() *EmbeddedSpecificationRepository {
	repo := &EmbeddedSpecificationRepository{
		history: make(map[string][]models.SandboxSpecification),
	}

	for _, spec := range defaultSpecs() {
		repo.append(spec)
	}

	return repo
}

// Get returns the latest specification for the provided id.
func (r *EmbeddedSpecificationRepository) Get(specID string) (models.SandboxSpecification, error) {
	versions, ok := r.history[specID]
	if !ok || len(versions) == 0 {
		return models.SandboxSpecification{}, errors.New("specification not found")
	}
	return versions[len(versions)-1], nil
}

// Save adds a new version for the provided specification.
func (r *EmbeddedSpecificationRepository) Save(spec models.SandboxSpecification) (models.SandboxSpecification, error) {
	r.append(spec)
	return spec, nil
}

// ListVersions lists all known versions for a specification id.
func (r *EmbeddedSpecificationRepository) ListVersions(specID string) ([]models.SandboxSpecification, error) {
	versions := r.history[specID]
	if len(versions) == 0 {
		return nil, nil
	}

	result := make([]models.SandboxSpecification, len(versions))
	copy(result, versions)
	return result, nil
}

// ListAll returns the latest version for every specification.
func (r *EmbeddedSpecificationRepository) ListAll() ([]models.SandboxSpecification, error) {
	if len(r.history) == 0 {
		return nil, nil
	}

	specs := make([]models.SandboxSpecification, 0, len(r.order))
	for _, id := range r.order {
		if versions := r.history[id]; len(versions) > 0 {
			specs = append(specs, versions[len(versions)-1])
		}
	}
	return specs, nil
}

// FilterByArchitecture returns specs matching the requested architecture.
func (r *EmbeddedSpecificationRepository) FilterByArchitecture(architecture string) ([]models.SandboxSpecification, error) {
	if architecture == "" {
		return r.ListAll()
	}

	all, err := r.ListAll()
	if err != nil {
		return nil, err
	}

	var matched []models.SandboxSpecification
	for _, spec := range all {
		if spec.Metadata != nil {
			if archValue, ok := spec.Metadata["arch"].(string); ok && strings.EqualFold(archValue, architecture) {
				matched = append(matched, spec)
				continue
			}
		}

		if strings.EqualFold(spec.DomainProfile.Arch, architecture) {
			matched = append(matched, spec)
		}
	}
	return matched, nil
}

func (r *EmbeddedSpecificationRepository) append(spec models.SandboxSpecification) {
	if _, exists := r.history[spec.ID]; !exists {
		r.order = append(r.order, spec.ID)
	}
	r.history[spec.ID] = append(r.history[spec.ID], spec)
}

func defaultSpecs() []models.SandboxSpecification {
	return []models.SandboxSpecification{
		makeSpec(
			"debian-bookworm-amd64",
			"amd64",
			"ttyS0",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-amd64/current/images/netboot/debian-installer/amd64/linux",
			"https://ftp.debian.org/debian/dists/bookworm/main/installer-amd64/current/images/netboot/debian-installer/amd64/initrd.gz",
			models.DomainProfile{
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
			models.DomainProfile{
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
			models.DomainProfile{
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
			models.DomainProfile{
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
			models.DomainProfile{
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
			models.DomainProfile{
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
			models.DomainProfile{
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
	domain models.DomainProfile,
) models.SandboxSpecification {
	domainCopy := domain
	if len(domain.ExtraArgs) > 0 {
		extra := make([]string, len(domain.ExtraArgs))
		copy(extra, domain.ExtraArgs)
		domainCopy.ExtraArgs = extra
	}

	return models.SandboxSpecification{
		ID:            specID,
		Version:       defaultVersion,
		OSRelease:     "debian-" + defaultRelease,
		DomainProfile: domainCopy,
		BuildProfile: models.BuildProfile{
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
		RunProfile:    runProfile(models.BootMethodBIOS),
		NetworkLayout: networkLayout(),
		InstallerAssets: map[string]string{
			"preseed_content":       preseedContent,
			"network_configuration": buildNetworkContent,
		},
		Metadata: map[string]any{
			"arch":        arch,
			"maintainer":  "embedded",
			"description": "Embedded Debian netinst profile for sandbox builds",
		},
	}
}

func strPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func runProfile(bootMethod models.BootMethod) models.RunProfile {
	return models.RunProfile{
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
