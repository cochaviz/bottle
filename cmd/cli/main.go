package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	simple "cochaviz/mime/configurations"
	"cochaviz/mime/setup"
)

func verifySetup() {
	if err := setup.Verify(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Please run 'mime setup' to initialize the configuraiton.\n")
		os.Exit(1)
	}
}

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "mime",
		Short: "CLI for 'mime': long-term monitoring of sandboxed botnet samples",
	}

	root.AddCommand(newBuildCommand(), newListCommand(), newSetupCommand())
	return root
}

func newBuildCommand() *cobra.Command {
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
			verifySetup()

			if err := simple.Build(specID, imageDir, artifactDir, connectionURI); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Build completed for %s\n", specID)
			return nil
		},
	}

	cmd.Flags().StringVar(&specID, "spec", "", "Specification identifier to build")
	cmd.Flags().StringVar(&imageDir, "image-dir", simple.DefaultImageDir, "Directory where images will be stored")
	cmd.Flags().StringVar(&artifactDir, "artifact-dir", simple.DefaultArtifactDir, "Directory to store build artifacts")
	cmd.Flags().StringVar(&connectionURI, "connect-uri", simple.DefaultConnectionURI, "Libvirt connection URI")

	cmd.MarkFlagRequired("spec")

	return cmd
}

func newListCommand() *cobra.Command {
	var imageDir string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available specifications and whether they have a local image",
		RunE: func(cmd *cobra.Command, args []string) error {
			verifySetup()

			if err := simple.List(imageDir); err != nil {
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&imageDir, "image-dir", simple.DefaultImageDir, "Directory where images are stored")

	return cmd
}

func newSetupCommand() *cobra.Command {
	var clearConfig bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Initialize the system with default configurations",
		RunE: func(cmd *cobra.Command, args []string) error {
			err := setup.Verify()
			if err == nil {
				fmt.Println("Already set up: use 'mime setup --clear' to clear configuration and reinitialize")
				os.Exit(0)
			}

			if clearConfig {
				if err := setup.ClearConfig(); err != nil {
					return fmt.Errorf("clear configuration: %w", err)
				}
			}

			if err := setup.SetupNetwork(context.Background()); err != nil {
				return fmt.Errorf("initialize networking: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&clearConfig, "clear", "C", false, "Remove existing setup configuration before initializing")

	return cmd
}
