package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	config "cochaviz/mime/config"
	"cochaviz/mime/internal/logging"
	"cochaviz/mime/internal/setup"
)

const defaultLogLevel = "warning"

func main() {
	var levelVar slog.LevelVar
	levelVar.Set(slog.LevelInfo)

	logger := logging.NewCLI(os.Stderr, &levelVar)
	slog.SetDefault(logger)

	root := newRootCommand(logger, &levelVar)
	if err := root.Execute(); err != nil {
		logger.Error("command execution failed", "error", err)
		os.Exit(1)
	}
}

func newRootCommand(logger *slog.Logger, levelVar *slog.LevelVar) *cobra.Command {
	setup.SetLogger(logger.With("component", "setup"))

	logLevel := defaultLogLevel

	root := &cobra.Command{
		Use:           "mime",
		Short:         "CLI for 'mime': long-term monitoring of sandboxed botnet samples",
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

	root.AddCommand(newSandboxCommand(logger), newSetupCommand(logger))
	return root
}

func verifySetup(logger *slog.Logger) error {
	logger = logger.With("action", "verify_setup")
	logger.Info("verifying setup state")
	if err := setup.Verify(); err != nil {
		logger.Error("setup verification failed", "error", err)
		logger.Info("run 'mime setup' to initialize the configuration")
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

			if err := config.BuildWithLogger(specID, imageDir, artifactDir, connectionURI, cmdLogger); err != nil {
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
	var imageDir string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available specifications and whether they have a local image",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdLogger := logger.With("command", "sandbox.list")

			if err := verifySetup(cmdLogger); err != nil {
				return err
			}

			cmdLogger.Info("listing specifications", "image_dir", imageDir)

			specs, built, err := config.List(imageDir)
			if err != nil {
				cmdLogger.Error("listing specifications failed", "error", err)
				return err
			}

			if len(specs) == 0 {
				cmdLogger.Warn("no specifications available", "image_dir", imageDir)
				return nil
			}

			for i, spec := range specs {
				fmt.Printf("%s\t(built: %t)\n", spec, built[i])
			}

			cmdLogger.Info("listed specifications", "count", len(specs))
			return nil
		},
	}

	cmd.Flags().StringVar(&imageDir, "image-dir", config.DefaultImageDir, "Directory where images are stored")

	return cmd
}

func newSetupCommand(logger *slog.Logger) *cobra.Command {
	var clearConfig bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Initialize the system with default configurations",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdLogger := logger.With("command", "setup")

			if err := setup.Verify(); err == nil {
				cmdLogger.Info("system already configured", "hint", "use 'mime setup --clear' to reinitialize")
				os.Exit(0)
			}

			if clearConfig {
				cmdLogger.Info("clearing existing configuration")
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
