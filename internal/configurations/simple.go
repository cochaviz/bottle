package simple

import (
	"fmt"
	"log/slog"
	"os"

	"cochaviz/mime/internal/drivers/build/libvirt"
	"cochaviz/mime/internal/logging"
	"cochaviz/mime/internal/models"
	embeddedrepositories "cochaviz/mime/internal/repositories/embedded"
	localrepositories "cochaviz/mime/internal/repositories/local"
	"cochaviz/mime/internal/services"
	"cochaviz/mime/internal/setup"
)

var DefaultArtifactDir = setup.StorageDir + "artifacts"
var DefaultImageDir = setup.StorageDir + "images"
var DefaultConnectionURI = "qemu:///system"

// Build executes the end-to-end flow to produce an image for the requested specification.
func Build(specificationID, imageDir, artifactDir, libvirtConnectionURI string) error {
	return BuildWithLogger(specificationID, imageDir, artifactDir, libvirtConnectionURI, nil)
}

// BuildWithLogger executes the end-to-end flow to produce an image for the requested specification using the provided logger.
func BuildWithLogger(specificationID, imageDir, artifactDir, libvirtConnectionURI string, logger *slog.Logger) error {
	logger = logging.Ensure(logger).With("component", "config.simple")

	if specificationID == "" {
		return fmt.Errorf("specification id is required")
	}
	if imageDir == "" {
		imageDir = DefaultArtifactDir
	}
	if artifactDir == "" {
		artifactDir = DefaultArtifactDir
	}

	if libvirtConnectionURI == "" {
		libvirtConnectionURI = DefaultConnectionURI
	}

	buildDir, err := os.MkdirTemp("", "mime-build-*")
	if err != nil {
		return fmt.Errorf("create build directory: %w", err)
	}
	defer os.RemoveAll(buildDir)

	buildService := services.BuildService{
		Logger: logger.With("service", "build"),
		EnvironmentPreparer: &libvirt.LibvirtBuildEnvironmentPreparer{
			BaseDir:            buildDir,
			StoragePoolCleaner: libvirt.LibvirtStoragePoolCleaner{},
		},
		BuildDriver: &libvirt.VirtInstallBuilder{
			Logger: logger.With("driver", "virt-install"),
		},
		SandboxSpecificationRepository: embeddedrepositories.NewEmbeddedSpecificationRepository(),
		ImageRepository: &localrepositories.LocalImageRepository{
			BaseDir: imageDir,
		},
		ArtifactStore: &localrepositories.LocalArtifactStore{
			BaseDir: artifactDir,
		},
	}

	if err := buildService.Run(&models.BuildRequest{SpecificationID: specificationID}); err != nil {
		return err
	}

	return nil
}

// List prints the available specifications and whether an image exists locally.
func List(imageDir string) error {
	return ListWithLogger(imageDir, nil)
}

// ListWithLogger logs the available specifications and whether an image exists locally using the provided logger.
func ListWithLogger(imageDir string, logger *slog.Logger) error {
	logger = logging.Ensure(logger).With("component", "config.simple")

	if imageDir == "" {
		imageDir = "/var/libvirt/mime/images"
	}

	imageRepository := &localrepositories.LocalImageRepository{BaseDir: imageDir}
	specificationRepository := embeddedrepositories.NewEmbeddedSpecificationRepository()

	specifications, err := specificationRepository.ListAll()
	if err != nil {
		return err
	}

	for _, specification := range specifications {
		latestImage, err := imageRepository.LatestForSpec(specification.ID)
		if err != nil {
			return err
		}

		logger.Info("specification status", "specification", specification.ID, "image_available", latestImage != nil)
	}

	return nil
}
