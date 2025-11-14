package libvirt

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"cochaviz/mime/internal/artifacts"
	"cochaviz/mime/internal/build"

	libvirt "libvirt.org/go/libvirt"
)

// Ensure VirtInstallBuilder satisfies the build driver interface.
var _ build.BuildDriver = (*VirtInstallBuilder)(nil)

// BuildConfig captures the inputs needed to run virt-install.
type BuildConfig struct {
	Console   string   `json:"console"`
	Arch      string   `json:"arch"`
	Machine   *string  `json:"machine"`
	CPUModel  *string  `json:"cpu_model"`
	RAMMB     int      `json:"ram_mb"`
	VCPUs     int      `json:"vcpus"`
	ExtraArgs []string `json:"extra_args"`

	DiskSizeGB int    `json:"disk_size_gb"`
	DiskFormat string `json:"disk_format"`

	NetworkName  string `json:"network_name"`
	NetworkModel string `json:"network_model"`

	UsePreseed bool   `json:"use_preseed"`
	KernelURL  string `json:"kernel_url"`
	InitrdURL  string `json:"initrd_url"`
	MirrorHost string `json:"mirror_host"`
	MirrorPath string `json:"mirror_path"`
	Release    string `json:"release"`

	ConnectURI string `json:"connect_uri"`

	OutputDir  string `json:"output_dir"`
	OutputPath string `json:"output_path"`
	HostArch   string `json:"host_arch"`

	PreseedPath              *string `json:"preseed_path"`
	NetworkConfigurationPath *string `json:"network_configuration_path"`
}

// VirtInstallBuilder provisions Debian-based images via virt-install.
type VirtInstallBuilder struct {
	Logger *slog.Logger
}

func (b *VirtInstallBuilder) logger() *slog.Logger {
	if b != nil && b.Logger != nil {
		return b.Logger
	}
	return slog.Default()
}

// Build runs the virt-install workflow using libvirt.
func (b *VirtInstallBuilder) Build(ctx build.BuildContext, env build.BuildEnvironment) (build.BuildOutput, error) {
	libvirtEnv, ok := env.(*LibvirtBuildEnvironment)
	if !ok {
		return build.BuildOutput{}, &build.BuildError{Message: "invalid environment type: expected *LibvirtBuildEnvironment"}
	}

	config, err := deriveConfig(ctx, *libvirtEnv)
	if err != nil {
		return build.BuildOutput{}, err
	}

	kernelArgs, err := buildKernelArgs(config)
	if err != nil {
		return build.BuildOutput{}, err
	}

	logger := b.logger().With(
		"specification", ctx.Spec.SandboxSpecification.ID,
		"release", config.Release,
		"arch", config.Arch,
	)
	logger.Info("starting virt-install build",
		"kernel_url", config.KernelURL,
		"initrd_url", config.InitrdURL,
		"output_path", config.OutputPath,
		"connect_uri", config.ConnectURI,
	)

	conn, err := libvirt.NewConnect(config.ConnectURI)
	if err != nil {
		return build.BuildOutput{}, &build.BuildError{Message: fmt.Sprintf("open libvirt connection %s: %v", config.ConnectURI, err)}
	}
	defer conn.Close()

	if err := b.ensureNetwork(logger, conn, config.NetworkName, config.NetworkConfigurationPath); err != nil {
		return build.BuildOutput{}, err
	}

	command, requiresEmulation := buildCommand(config, kernelArgs)
	if requiresEmulation {
		logger.Warn("using QEMU software emulation",
			"host_arch", config.HostArch,
			"requested_arch", config.Arch,
		)
	}
	logger.Info("running virt-install",
		"command", strings.Join(command, " "),
	)
	if err := runCommand(command); err != nil {
		return build.BuildOutput{}, err
	}

	buildArtifacts := []artifacts.Artifact{}

	if config.UsePreseed && config.PreseedPath != nil {
		buildArtifacts = append(buildArtifacts, artifacts.Artifact{
			// save as local file
			URI:      fmt.Sprintf("file://%s", *config.PreseedPath),
			Kind:     artifacts.TextArtifact,
			Metadata: map[string]any{"injected": true},
		})
	}

	imageArtifact := artifacts.Artifact{
		URI:         fmt.Sprintf("file://%s", config.OutputPath),
		Kind:        artifacts.ImageArtifact,
		Metadata:    map[string]any{"injected": true},
		ContentType: "qcow2",
	}

	metadata := map[string]any{
		"release":     config.Release,
		"arch":        config.Arch,
		"kernel_url":  config.KernelURL,
		"initrd_url":  config.InitrdURL,
		"kernel_args": kernelArgs,
		"network":     config.NetworkName,
		"connect_uri": config.ConnectURI,
	}

	return build.BuildOutput{
		DiskImage:          imageArtifact,
		CompanionArtifacts: buildArtifacts,
		Metadata:           metadata,
	}, nil
}

func deriveConfig(ctx build.BuildContext, env LibvirtBuildEnvironment) (*BuildConfig, error) {
	spec := ctx.Spec
	domainProfile := spec.SandboxSpecification.DomainProfile

	config := &BuildConfig{
		Console:    spec.Profile.Console,
		Release:    spec.Profile.Release,
		Arch:       canonicalizeArch(domainProfile.Arch),
		DiskSizeGB: spec.Profile.DiskSizeGB,
		DiskFormat: "qcow2",
		ExtraArgs:  append([]string(nil), domainProfile.ExtraArgs...),

		KernelURL:   spec.Profile.KernelURL,
		InitrdURL:   spec.Profile.InitrdURL,
		MirrorHost:  spec.Profile.MirrorHost,
		MirrorPath:  spec.Profile.MirrorPath,
		NetworkName: spec.Profile.NetworkName,

		NetworkModel: domainProfile.NetworkModel,
		RAMMB:        domainProfile.RAMMB,
		VCPUs:        domainProfile.VCPUs,
		Machine:      domainProfile.Machine,
		CPUModel:     domainProfile.CPUModel,

		UsePreseed:               spec.Profile.PreseedEnabled,
		PreseedPath:              env.PreseedConfigPath,
		NetworkConfigurationPath: env.NetworkConfigurationPath,
		OutputDir:                env.WorkDir,
		ConnectURI:               env.ConnectURI,
		HostArch:                 detectHostArch(),
	}

	if err := config.applyOverrides(ctx.Overrides); err != nil {
		return nil, err
	}

	if err := config.resolvePaths(); err != nil {
		return nil, err
	}

	return config, nil
}

func (cfg *BuildConfig) applyOverrides(overrides map[string]any) error {
	if len(overrides) == 0 {
		return nil
	}

	raw, err := json.Marshal(overrides)
	if err != nil {
		return fmt.Errorf("marshal overrides: %w", err)
	}

	type configAlias BuildConfig
	alias := configAlias(*cfg)
	if err := json.Unmarshal(raw, &alias); err != nil {
		return fmt.Errorf("apply overrides: %w", err)
	}

	*cfg = BuildConfig(alias)
	return nil
}

func (cfg *BuildConfig) resolvePaths() error {
	if cfg.OutputDir == "" {
		return &build.BuildError{Message: "output directory is required"}
	}

	absDir, err := filepath.Abs(cfg.OutputDir)
	if err != nil {
		return fmt.Errorf("resolve output dir: %w", err)
	}

	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	cfg.OutputDir = absDir

	if cfg.OutputPath == "" {
		filename := fmt.Sprintf("sandbox-%s-%s.%s", cfg.Release, cfg.Arch, cfg.DiskFormat)
		cfg.OutputPath = filepath.Join(cfg.OutputDir, filename)
	} else if !filepath.IsAbs(cfg.OutputPath) {
		cfg.OutputPath = filepath.Join(cfg.OutputDir, cfg.OutputPath)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.OutputPath), 0o755); err != nil {
		return fmt.Errorf("ensure output path dir: %w", err)
	}

	if cfg.UsePreseed {
		if cfg.PreseedPath == nil || *cfg.PreseedPath == "" {
			return &build.BuildError{Message: "preseed path must be provided when preseed injection is enabled"}
		}

		if _, err := os.Stat(*cfg.PreseedPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return &build.BuildError{Message: fmt.Sprintf("preseed file not found: %s", *cfg.PreseedPath)}
			}
			return fmt.Errorf("stat preseed file: %w", err)
		}
	}

	return nil
}

// buildKernelArgs builds the kernel arguments for the sandbox.
func buildKernelArgs(cfg *BuildConfig) ([]string, error) {
	args := []string{
		"auto=true",
		"priority=critical",
		"debian-installer/locale=en_US",
		"kbd-chooser/method=us",
		"DEBIAN_FRONTEND=text",
		fmt.Sprintf("mirror/http/hostname=%s", cfg.MirrorHost),
		fmt.Sprintf("mirror/http/directory=%s", cfg.MirrorPath),
		fmt.Sprintf("console=%s,115200n8", cfg.Console),
	}

	if cfg.UsePreseed {
		if cfg.PreseedPath == nil || *cfg.PreseedPath == "" {
			return nil, &build.BuildError{Message: "preseed path must be resolved before building kernel arguments"}
		}
		args = append([]string{fmt.Sprintf("preseed/file=/%s", filepath.Base(*cfg.PreseedPath))}, args...)
	} else {
		if len(args) >= 2 {
			args = args[2:]
		}
	}

	if len(cfg.ExtraArgs) > 0 {
		args = append(args, cfg.ExtraArgs...)
	}

	return args, nil
}

// ensureNetwork ensures that the specified network exists and is active.
func (b *VirtInstallBuilder) ensureNetwork(logger *slog.Logger, conn *libvirt.Connect, name string, xmlPath *string) error {
	network, err := conn.LookupNetworkByName(name)
	if err != nil {
		if xmlPath == nil || *xmlPath == "" {
			return &build.BuildError{Message: fmt.Sprintf("network %q not found and no XML configuration provided", name)}
		}

		data, readErr := os.ReadFile(*xmlPath)
		if readErr != nil {
			return fmt.Errorf("read network xml: %w", readErr)
		}

		network, err = conn.NetworkDefineXML(string(data))
		if err != nil {
			return fmt.Errorf("define network: %w", err)
		}
		logger.Info("defined libvirt network", "network", name)
	}

	defer network.Free()

	active, err := network.IsActive()
	if err != nil {
		return fmt.Errorf("query network active: %w", err)
	}

	if !active {
		// First, we have to destroy the network if it's active
		if err := network.Destroy(); err != nil {
			return fmt.Errorf("destroy network: %w", err)
		}
		// Only then, we can recreate the network (otherwise we'll get an error)
		if err := network.Create(); err != nil {
			return fmt.Errorf("start network: %w", err)
		}
		logger.Info("started libvirt network", "network", name)
	}

	if err := network.SetAutostart(true); err != nil {
		logger.Warn("unable to set network autostart", "network", name, "error", err)
	}

	return nil
}

// buildCommand builds the command line arguments for virt-install.
func buildCommand(cfg *BuildConfig, kernelArgs []string) ([]string, bool) {
	diskSegment := fmt.Sprintf("path=%s,format=%s,size=%d", cfg.OutputPath, cfg.DiskFormat, cfg.DiskSizeGB)
	cmd := []string{
		"virt-install",
		"--connect", cfg.ConnectURI,
		"--name", fmt.Sprintf("sandbox-builder-%s-%s", cfg.Release, cfg.Arch),
		"--memory", fmt.Sprintf("%d", cfg.RAMMB),
		"--vcpus", fmt.Sprintf("%d", cfg.VCPUs),
		"--disk", diskSegment,
		"--arch", cfg.Arch,
		"--graphics", "none",
		"--console", "pty,target_type=serial",
		"--install", fmt.Sprintf("kernel=%s,initrd=%s", cfg.KernelURL, cfg.InitrdURL),
		"--extra-args", strings.Join(kernelArgs, " "),
		"--network", fmt.Sprintf("network=%s,model=%s", cfg.NetworkName, cfg.NetworkModel),
		"--os-variant", osVariant(cfg.Release),
		"--wait", "-1",
		"--transient",
	}

	if cfg.Machine != nil && *cfg.Machine != "" {
		cmd = append(cmd, "--machine", *cfg.Machine)
	}

	if cfg.CPUModel != nil && *cfg.CPUModel != "" {
		cmd = append(cmd, "--cpu", *cfg.CPUModel)
	}

	if cfg.UsePreseed && cfg.PreseedPath != nil {
		cmd = append(cmd, "--initrd-inject", *cfg.PreseedPath)
		cmd = append(cmd, "--noautoconsole")
	}

	requireEmulation := false
	if cfg.HostArch == "" || cfg.HostArch != cfg.Arch {
		requireEmulation = true
		cmd = append(cmd, "--virt-type", "qemu")
	}

	return cmd, requireEmulation
}

// runCommand runs the virt-install command with the provided arguments.
func runCommand(args []string) error {
	if len(args) == 0 {
		return &build.BuildError{Message: "no command provided"}
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return &build.BuildError{Message: fmt.Sprintf("virt-install failed: %v", err)}
	}

	return nil
}

// osVariant returns the appropriate OS variant for the given release.
func osVariant(release string) string {
	switch strings.ToLower(release) {
	case "bookworm":
		return "debian12"
	case "bullseye":
		return "debian11"
	case "buster":
		return "debian10"
	case "sid", "unstable":
		return "debian-unstable"
	case "testing":
		return "debian-testing"
	default:
		return "debian12"
	}
}

// canonicalizeArch canonicalizes the given architecture string.
func canonicalizeArch(arch string) string {
	normalized := strings.ToLower(strings.TrimSpace(arch))
	switch normalized {
	case "x86_64", "amd64":
		return "x86_64"
	case "arm64", "aarch64":
		return "aarch64"
	case "armhf":
		return "armv7l"
	default:
		if normalized == "" {
			return runtime.GOARCH
		}
		return normalized
	}
}

func detectHostArch() string {
	return canonicalizeArch(runtime.GOARCH)
}
