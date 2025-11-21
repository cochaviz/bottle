package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/cochaviz/bottle/arch"
	config "github.com/cochaviz/bottle/config"
	daemon "github.com/cochaviz/bottle/daemon"
	"github.com/cochaviz/bottle/internal/analysis"
	"github.com/cochaviz/bottle/internal/logging"
	"github.com/cochaviz/bottle/internal/sandbox"
	"github.com/cochaviz/bottle/internal/setup"
)

const defaultLogLevel = "info"

func StartCLI() {
	var levelVar slog.LevelVar
	levelVar.Set(slog.LevelInfo)

	logger := logging.NewCLI(os.Stderr, &levelVar)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := newRootCommand(logger, &levelVar)
	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Warn("command interrupted", "error", err)
			os.Exit(130)
		}
		logger.Error("command execution failed", "error", err)
		os.Exit(1)
	}
}

func newRootCommand(logger *slog.Logger, levelVar *slog.LevelVar) *cobra.Command {
	setup.SetLogger(logger.With("component", "setup"))

	logLevel := defaultLogLevel

	root := &cobra.Command{
		Use:           "bottle",
		Short:         "CLI for 'bottle': long-term monitoring of sandboxed botnet samples",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	root.PersistentFlags().StringVar(&logLevel, "log-level", defaultLogLevel, "Set log verbosity (debug, info, warning, error)")
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		level, err := parseLogLevel(logLevel)
		if err != nil {
			return err
		}
		if levelVar != nil {
			levelVar.Set(level)
		}
		return nil
	}

	root.AddCommand(
		newSandboxCommand(logger),
		newSetupCommand(logger),
		newAnalysisCommand(logger),
		newDaemonCommand(logger),
		newNetworkCommand(logger),
	)
	return root
}

func verifySetup(logger *slog.Logger) error {
	logger = logger.With("action", "verify_setup")
	logger.Info("verifying setup state")
	if err := setup.Verify(); err != nil {
		logger.Error("setup verification failed", "error", err)
		logger.Info("run 'bottle setup' to initialize the configuration")
		return err
	}
	logger.Info("setup verification succeeded")
	return nil
}

func newSandboxCommand(logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Manage sandbox images and runs",
	}

	cmd.AddCommand(
		newSandboxBuildCommand(logger),
		newSandboxRunCommand(logger),
		newSandboxListCommand(logger),
		newSandboxRemoveCommand(logger),
	)
	return cmd
}

func newSandboxBuildCommand(logger *slog.Logger) *cobra.Command {
	var (
		imageDir      string
		artifactDir   string
		connectionURI string
	)

	cmd := &cobra.Command{
		Use:   "build <spec-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Build a sandbox image for the specified specification",
		RunE: func(cmd *cobra.Command, args []string) error {
			specID := strings.TrimSpace(args[0])
			if specID == "" {
				return fmt.Errorf("specification is required")
			}

			cmdLogger := logger.With("command", "sandbox.build", "specification", specID)

			if err := verifySetup(cmdLogger); err != nil {
				return err
			}

			cmdLogger.Info("starting build", "image_dir", imageDir, "artifact_dir", artifactDir, "connect_uri", connectionURI)

			if err := config.BuildSandbox(specID, imageDir, artifactDir, connectionURI, cmdLogger); err != nil {
				cmdLogger.Error("build failed", "error", err)
				return err
			}

			cmdLogger.Info("build completed")
			return nil
		},
	}

	cmd.Flags().StringVar(&imageDir, "image-dir", config.DefaultImageDir, "Directory where images will be stored")
	cmd.Flags().StringVar(&artifactDir, "artifact-dir", config.DefaultArtifactDir, "Directory to store build artifacts")
	cmd.Flags().StringVar(&connectionURI, "connect-uri", config.DefaultConnectionURI, "Libvirt connection URI")

	return cmd
}

func newSandboxRunCommand(logger *slog.Logger) *cobra.Command {
	var (
		runDir        string
		connectionURI string
		domainName    string
		imageDir      string
		sampleDir     string
	)

	cmd := &cobra.Command{
		Use:   "run <spec-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Acquire and start a sandbox for the specified specification",
		RunE: func(cmd *cobra.Command, args []string) error {
			specID := strings.TrimSpace(args[0])
			if specID == "" {
				return fmt.Errorf("specification is required")
			}

			cmdLogger := logger.With("command", "sandbox.run", "specification", specID)
			if err := verifySetup(cmdLogger); err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			cmdLogger.Info("starting sandbox worker; press Ctrl+C to stop", "run_dir", runDir)
			if err := config.RunSandbox(ctx, specID, imageDir, runDir, sampleDir, domainName, connectionURI, cmdLogger); err != nil {
				cmdLogger.Error("sandbox worker failed", "error", err)
				return err
			}

			cmdLogger.Info("sandbox worker completed")
			return nil
		},
	}

	cmd.Flags().StringVar(&imageDir, "image-dir", config.DefaultImageDir, "Directory where images are stored")
	cmd.Flags().StringVar(&runDir, "run-dir", config.DefaultRunDir, "Directory to store sandbox run state")
	cmd.Flags().StringVar(&connectionURI, "connect-uri", config.DefaultConnectionURI, "Libvirt connection URI")
	cmd.Flags().StringVar(&domainName, "domain", "", "Optional domain name override")
	cmd.Flags().StringVar(&sampleDir, "sample-dir", "", "Directory containing sample files to mount into the sandbox")

	return cmd
}

func newSandboxListCommand(logger *slog.Logger) *cobra.Command {
	var (
		imageDir string
		showAll  bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available specifications and whether they have a local image",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdLogger := logger.With("command", "sandbox.list")

			if err := verifySetup(cmdLogger); err != nil {
				return err
			}

			cmdLogger.Info("listing specifications", "image_dir", imageDir)

			entries, err := config.List(imageDir, showAll)
			if err != nil {
				cmdLogger.Error("listing specifications failed", "error", err)
				return err
			}

			if len(entries) == 0 {
				cmdLogger.Warn("no specifications available", "image_dir", imageDir)
				return nil
			}

			for _, entry := range entries {
				fmt.Printf("%s\t(built: %t)\n", entry.SpecificationID, entry.Built)
				if showAll && len(entry.Images) > 0 {
					for _, image := range entry.Images {
						fmt.Printf("  - %s\tcreated: %s\n", image.ID, image.CreatedAt.Format(time.RFC3339))
					}
				}
			}

			cmdLogger.Info("listed specifications", "count", len(entries))
			return nil
		},
	}

	cmd.Flags().StringVar(&imageDir, "image-dir", config.DefaultImageDir, "Directory where images are stored")
	cmd.Flags().BoolVar(&showAll, "all", false, "Show all versions for each specification")

	return cmd
}

func newSandboxRemoveCommand(logger *slog.Logger) *cobra.Command {
	var (
		imageDir    string
		artifactDir string
	)

	cmd := &cobra.Command{
		Use:   "remove <image-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Remove a sandbox image and its qcow2 artifact",
		RunE: func(cmd *cobra.Command, args []string) error {
			imageID := strings.TrimSpace(args[0])
			if imageID == "" {
				return fmt.Errorf("image id is required")
			}

			cmdLogger := logger.With("command", "sandbox.remove", "image", imageID)
			if err := verifySetup(cmdLogger); err != nil {
				return err
			}

			cmdLogger.Info("removing sandbox image", "image_dir", imageDir, "artifact_dir", artifactDir)
			if err := config.RemoveSandboxImage(imageID, imageDir, artifactDir, cmdLogger); err != nil {
				cmdLogger.Error("remove failed", "error", err)
				return err
			}

			cmdLogger.Info("sandbox image removed")
			return nil
		},
	}

	cmd.Flags().StringVar(&imageDir, "image-dir", config.DefaultImageDir, "Directory where image metadata are stored")
	cmd.Flags().StringVar(&artifactDir, "artifact-dir", config.DefaultArtifactDir, "Directory where image artifacts are stored")

	return cmd
}

func newAnalysisCommand(logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analysis",
		Short: "Run analysis workflows for malware samples",
	}

	cmd.AddCommand(newAnalysisRunCommand(logger))
	return cmd
}

func newAnalysisRunCommand(logger *slog.Logger) *cobra.Command {
	var (
		imageDir               string
		runDir                 string
		connectionURI          string
		c2Address              string
		overrideArch           string
		sampleArgs             []string
		instrumentationConfig  string
		sampleExecutionTimeout = analysis.DefaultSampleExecutionTimeout
		sandboxLifetime        = analysis.DefaultSandboxLifetime
	)

	cmd := &cobra.Command{
		Use:   "run <sample-path>",
		Args:  cobra.ExactArgs(1),
		Short: "Execute a sample inside the sandbox analysis workflow",
		RunE: func(cmd *cobra.Command, args []string) error {
			samplePath := strings.TrimSpace(args[0])
			if samplePath == "" {
				return fmt.Errorf("sample path is required")
			}
			absSample, err := filepath.Abs(samplePath)
			if err != nil {
				return fmt.Errorf("resolve sample path: %w", err)
			}
			info, err := os.Stat(absSample)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("sample %s does not exist", absSample)
				}
				return fmt.Errorf("stat sample: %w", err)
			}
			if info.IsDir() {
				return fmt.Errorf("sample path %s is a directory; provide a file", absSample)
			}

			flatSampleArgs := flattenSampleArgs(sampleArgs)
			cmdLogger := logger.With("command", "analysis.run", "sample", filepath.Base(absSample))
			if err := verifySetup(cmdLogger); err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			var requestedArch arch.Architecture
			if strings.TrimSpace(overrideArch) != "" {
				parsedArch, err := arch.Parse(overrideArch)
				if err != nil {
					return err
				}
				requestedArch = parsedArch
			}

			if err := config.RunAnalysis(
				ctx,
				absSample,
				c2Address,
				imageDir,
				runDir,
				connectionURI,
				requestedArch,
				flatSampleArgs,
				instrumentationConfig,
				sampleExecutionTimeout,
				sandboxLifetime,
				cmdLogger,
			); err != nil {
				return err
			}

			cmdLogger.Info("analysis run completed")
			return nil
		},
	}

	cmd.Flags().StringVar(&imageDir, "image-dir", config.DefaultImageDir, "Directory where images are stored")
	cmd.Flags().StringVar(&runDir, "run-dir", config.DefaultRunDir, "Directory to store sandbox run state")
	cmd.Flags().StringVar(&connectionURI, "connect-uri", config.DefaultConnectionURI, "Libvirt connection URI")
	cmd.Flags().StringVar(&c2Address, "c2", "", "Optional C2 address to inject into the analysis")
	cmd.Flags().StringVar(&overrideArch, "arch", "", "Override sample architecture (e.g., x86_64, arm64)")
	cmd.Flags().StringVar(&instrumentationConfig, "instrumentation", "", "Path to YAML instrumentation config")
	cmd.Flags().StringArrayVar(&sampleArgs, "sample-args", nil, "Argument to pass to the sample; repeat flag to add additional args")
	cmd.Flags().DurationVar(&sampleExecutionTimeout, "sample-timeout", analysis.DefaultSampleExecutionTimeout, "Timeout for sample execution (e.g., 2m)")
	cmd.Flags().DurationVar(&sandboxLifetime, "sandbox-lifetime", analysis.DefaultSandboxLifetime, "How long to keep the sandbox running before stopping it")

	return cmd
}

func flattenSampleArgs(args []string) []string {
	var flat []string
	for _, arg := range args {
		for _, field := range strings.Fields(arg) {
			flat = append(flat, field)
		}
	}
	return flat
}

func newDaemonCommand(logger *slog.Logger) *cobra.Command {
	var socketPath string
	resolveSocket := func() string {
		path := strings.TrimSpace(socketPath)
		if path == "" {
			return daemon.DefaultSocketPath
		}
		return path
	}

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the bottle analysis daemon",
	}
	cmd.PersistentFlags().StringVar(&socketPath, "socket", daemon.DefaultSocketPath, "Path to daemon control socket")

	cmd.AddCommand(
		newDaemonServeCommand(logger, resolveSocket),
		newDaemonStartAnalysisCommand(logger, resolveSocket),
		newDaemonStopAnalysisCommand(resolveSocket),
		newDaemonListCommand(resolveSocket),
		newDaemonInspectCommand(resolveSocket),
		newDaemonCleanupCommand(resolveSocket),
	)

	return cmd
}

func newDaemonServeCommand(logger *slog.Logger, socketPath func() string) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the daemon server",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			d := daemon.New(socketPath(), logger)
			logger.Info("starting daemon", "socket", socketPath())
			if err := d.Start(ctx); err != nil {
				return err
			}
			logger.Info("daemon stopped")
			return nil
		},
	}
}

func newDaemonStartAnalysisCommand(logger *slog.Logger, socketPath func() string) *cobra.Command {
	var (
		imageDir               string
		runDir                 string
		connectionURI          string
		c2Address              string
		overrideArch           string
		sampleArgs             []string
		instrumentationConfig  string
		sampleExecutionTimeout = analysis.DefaultSampleExecutionTimeout
		sandboxLifetime        = analysis.DefaultSandboxLifetime
	)

	cmd := &cobra.Command{
		Use:   "start <sample-path>",
		Args:  cobra.ExactArgs(1),
		Short: "Request the daemon to start an analysis",
		RunE: func(cmd *cobra.Command, args []string) error {
			samplePath := strings.TrimSpace(args[0])
			if samplePath == "" {
				return fmt.Errorf("sample path is required")
			}
			var requestedArch arch.Architecture
			if strings.TrimSpace(overrideArch) != "" {
				parsedArch, err := arch.Parse(overrideArch)
				if err != nil {
					return err
				}
				requestedArch = parsedArch
			}
			req := daemon.StartAnalysisRequest{
				SamplePath:      samplePath,
				C2Address:       c2Address,
				ImageDir:        imageDir,
				RunDir:          runDir,
				ConnectionURI:   connectionURI,
				OverrideArch:    requestedArch,
				SampleArgs:      flattenSampleArgs(sampleArgs),
				Instrumentation: instrumentationConfig,
				SampleTimeout:   sampleExecutionTimeout,
				SandboxLifetime: sandboxLifetime,
			}

			client := daemon.NewClient(socketPath())
			id, err := client.StartAnalysis(req)
			if err != nil {
				return err
			}
			logger.Info("analysis scheduled", "id", id)
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		},
	}

	cmd.Flags().StringVar(&imageDir, "image-dir", config.DefaultImageDir, "Directory where images are stored")
	cmd.Flags().StringVar(&runDir, "run-dir", config.DefaultRunDir, "Directory to store sandbox run state")
	cmd.Flags().StringVar(&connectionURI, "connect-uri", config.DefaultConnectionURI, "Libvirt connection URI")
	cmd.Flags().StringVar(&c2Address, "c2", "", "Optional C2 address to inject into the analysis")
	cmd.Flags().StringVar(&overrideArch, "arch", "", "Override sample architecture (e.g., x86_64, arm64)")
	cmd.Flags().StringVar(&instrumentationConfig, "instrumentation", "", "Path to YAML instrumentation config (repeat to run multiple)")
	cmd.Flags().DurationVar(&sampleExecutionTimeout, "sample-timeout", analysis.DefaultSampleExecutionTimeout, "Timeout for sample execution (e.g., 2m)")
	cmd.Flags().DurationVar(&sandboxLifetime, "sandbox-lifetime", analysis.DefaultSandboxLifetime, "How long to keep the sandbox running before stopping it")
	cmd.Flags().StringArrayVar(&sampleArgs, "sample-args", nil, "Argument to pass to the sample; repeat flag to add additional args")

	return cmd
}

func newDaemonStopAnalysisCommand(socketPath func() string) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <id>",
		Args:  cobra.ExactArgs(1),
		Short: "Request the daemon to stop an analysis",
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			client := daemon.NewClient(socketPath())
			if err := client.StopAnalysis(id); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "stopped", id)
			return nil
		},
	}
}

func newDaemonListCommand(socketPath func() string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List analyses managed by the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := daemon.NewClient(socketPath())
			statuses, err := client.List()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(statuses) == 0 {
				fmt.Fprintln(out, "no analyses")
				return nil
			}
			for _, status := range statuses {
				state := "running"
				if !status.Running {
					state = "completed"
					if status.Error != "" {
						state = fmt.Sprintf("failed (%s)", status.Error)
					}
					if status.CompletedAt != nil {
						state = fmt.Sprintf("%s at %s", state, status.CompletedAt.Format(time.RFC3339))
					}
				}
				fmt.Fprintf(out, "%s\t%s\t%s\n", status.ID, status.Sample, state)
			}
			return nil
		},
	}
}

func newDaemonInspectCommand(socketPath func() string) *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <id>",
		Args:  cobra.ExactArgs(1),
		Short: "Inspect a single analysis managed by the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("id is required")
			}
			client := daemon.NewClient(socketPath())
			detail, err := client.Inspect(id)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			status := detail.Status
			fmt.Fprintf(out, "ID:\t%s\n", status.ID)
			fmt.Fprintf(out, "Sample:\t%s\n", status.Sample)
			if status.C2Ip != "" {
				fmt.Fprintf(out, "C2:\t%s\n", status.C2Ip)
			}
			fmt.Fprintf(out, "Started:\t%s\n", status.StartedAt.Format(time.RFC3339))
			if status.CompletedAt != nil {
				fmt.Fprintf(out, "Completed:\t%s\n", status.CompletedAt.Format(time.RFC3339))
			}
			fmt.Fprintf(out, "Running:\t%t\n", status.Running)
			if status.Error != "" {
				fmt.Fprintf(out, "Error:\t%s\n", status.Error)
			}
			fmt.Fprintf(out, "Runtime:\t%s\n", detail.Duration)

			opts := detail.Options
			fmt.Fprintln(out, "Options:")
			printOption := func(key, value string) {
				if strings.TrimSpace(value) == "" {
					return
				}
				fmt.Fprintf(out, "  %s: %s\n", key, value)
			}
			printDuration := func(key string, value time.Duration) {
				if value <= 0 {
					return
				}
				fmt.Fprintf(out, "  %s: %s\n", key, value)
			}
			printOption("ImageDir", opts.ImageDir)
			printOption("RunDir", opts.RunDir)
			printOption("ConnectionURI", opts.ConnectionURI)
			printOption("OverrideArch", opts.OverrideArch.String())
			if len(opts.SampleArgs) > 0 {
				fmt.Fprintf(out, "  SampleArgs: %s\n", strings.Join(opts.SampleArgs, " "))
			}
			printOption("Instrumentation", opts.Instrumentation)
			printDuration("SampleTimeout", opts.SampleTimeout)
			printDuration("SandboxLifetime", opts.SandboxLifetime)
			printOption("LogLevel", opts.LogLevel)
			return nil
		},
	}
}

func newDaemonCleanupCommand(socketPath func() string) *cobra.Command {
	return &cobra.Command{
		Use:   "cleanup",
		Short: "Remove completed analyses from the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := daemon.NewClient(socketPath())
			removed, err := client.CleanupInactive()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %d completed worker(s)\n", removed)
			return nil
		},
	}
}

func newNetworkCommand(logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "network",
		Short: "Inspect networking state",
	}
	cmd.AddCommand(
		newNetworkLeasesCommand(logger),
		newNetworkWhitelistsCommand(),
	)
	return cmd
}

func newNetworkLeasesCommand(logger *slog.Logger) *cobra.Command {
	var (
		connectionURI string
		networkName   string
	)
	cmd := &cobra.Command{
		Use:   "leases",
		Short: "List DHCP leases for a libvirt network",
		RunE: func(cmd *cobra.Command, args []string) error {
			connectionURI = strings.TrimSpace(connectionURI)
			if connectionURI == "" {
				connectionURI = config.DefaultConnectionURI
			}
			networkName = strings.TrimSpace(networkName)
			if networkName == "" {
				networkName = sandbox.DefaultNetworkName
			}

			leases, err := sandbox.ListNetworkLeases(connectionURI, networkName)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			if len(leases) == 0 {
				fmt.Fprintln(out, "no leases")
				return nil
			}
			fmt.Fprintf(out, "Network: %s (URI: %s)\n", networkName, connectionURI)
			fmt.Fprintln(out, "MAC\tIP")
			for _, lease := range leases {
				fmt.Fprintf(out, "%s\t%s\n", strings.ToLower(strings.TrimSpace(lease.MAC)), lease.IP)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&connectionURI, "connect-uri", config.DefaultConnectionURI, "Libvirt connection URI")
	cmd.Flags().StringVar(&networkName, "network", sandbox.DefaultNetworkName, "Libvirt network name")
	return cmd
}

func newNetworkWhitelistsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "whitelists",
		Short: "List firewall whitelist rules for sandbox traffic",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := analysis.ListWhitelistedIPs()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(entries) == 0 {
				fmt.Fprintln(out, "no whitelisted entries")
				return nil
			}
			fmt.Fprintln(out, "VM_IP\tDEST_IP")
			for _, entry := range entries {
				fmt.Fprintf(out, "%s\t%s\n", entry.VMIP, entry.DestIP)
			}
			return nil
		},
	}
}

func newSetupCommand(logger *slog.Logger) *cobra.Command {
	var clearConfig bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Initialize the system with default configurations",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdLogger := logger.With("command", "setup")

			alreadyConfigured := false
			if err := setup.Verify(); err == nil {
				alreadyConfigured = true
			}

			if alreadyConfigured && !clearConfig {
				cmdLogger.Info("system already configured", "hint", "use 'bottle setup --clear' to reinitialize")
				os.Exit(0)
			}

			if clearConfig {
				logArgs := []any{}
				if alreadyConfigured {
					logArgs = append(logArgs, "action", "reinitializing existing configuration")
				}
				cmdLogger.Info("clearing existing configuration", logArgs...)
				if err := setup.ClearConfig(); err != nil {
					cmdLogger.Error("clear configuration failed", "error", err)
					return fmt.Errorf("clear configuration: %w", err)
				}
				cmdLogger.Info("existing configuration cleared")
			}

			if err := setup.SetupNetwork(context.Background()); err != nil {
				cmdLogger.Error("network initialization failed", "error", err)
				return fmt.Errorf("initialize networking: %w", err)
			}
			cmdLogger.Info("network initialization completed")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&clearConfig, "clear", "C", false, "Remove existing setup configuration before initializing")

	return cmd
}

func parseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error", "err":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level %q", value)
	}
}
