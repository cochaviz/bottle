package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	config "cochaviz/mime/config"
	"cochaviz/mime/internal/logging"
	"cochaviz/mime/internal/setup"
)

func main() {
	var levelVar slog.LevelVar
	levelVar.Set(slog.LevelInfo)

	logger := logging.NewCLI(os.Stderr, &levelVar)
	slog.SetDefault(logger)

	root := newRootCommand(logger, &levelVar)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCommand(logger *slog.Logger, levelVar *slog.LevelVar) *cobra.Command {
	setup.SetLogger(logger.With("component", "setup"))

	logLevel := "info"

	root := &cobra.Command{
		Use:           "mime",
		Short:         "CLI for 'mime': long-term monitoring of sandboxed botnet samples",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	root.PersistentFlags().StringVar(&logLevel, "log-level", logLevel, "Set log verbosity (debug, info, warn, error)")
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

	root.AddCommand(newBuildCommand(logger), newListCommand(logger), newSetupCommand(logger))
	return root
}

func verifySetup(logger *slog.Logger) error {
	if err := setup.Verify(); err != nil {
		logger.Error("setup verification failed", "error", err)
		logger.Info("run 'mime setup' to initialize the configuration")
		return err
	}
	return nil
}

func newBuildCommand(logger *slog.Logger) *cobra.Command {
	var (
		specID        string
		imageDir      string
		artifactDir   string
		connectionURI string
	)

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build a sandbox image for the specified specification",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdLogger := logger.With("command", "build", "specification", specID)

			if err := verifySetup(cmdLogger); err != nil {
				return err
			}

			if err := config.BuildWithLogger(specID, imageDir, artifactDir, connectionURI, cmdLogger); err != nil {
				cmdLogger.Error("build failed", "error", err)
				return err
			}

			cmdLogger.Info("build completed")
			return nil
		},
	}

	cmd.Flags().StringVar(&specID, "spec", "", "Specification identifier to build")
	cmd.Flags().StringVar(&imageDir, "image-dir", config.DefaultImageDir, "Directory where images will be stored")
	cmd.Flags().StringVar(&artifactDir, "artifact-dir", config.DefaultArtifactDir, "Directory to store build artifacts")
	cmd.Flags().StringVar(&connectionURI, "connect-uri", config.DefaultConnectionURI, "Libvirt connection URI")

	cmd.MarkFlagRequired("spec")

	return cmd
}

func newListCommand(logger *slog.Logger) *cobra.Command {
	var imageDir string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available specifications and whether they have a local image",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmdLogger := logger.With("command", "list")

			if err := verifySetup(cmdLogger); err != nil {
				return err
			}

			specs, built, err := config.List(imageDir)
			if err != nil {
				cmdLogger.Error("listing specifications failed", "error", err)
				return err
			}

			for i, spec := range specs {
				fmt.Printf("%s\t(built: %t)\n", spec, built[i])
			}

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
				if err := setup.ClearConfig(); err != nil {
					cmdLogger.Error("clear configuration failed", "error", err)
					return fmt.Errorf("clear configuration: %w", err)
				}
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
