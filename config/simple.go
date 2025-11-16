package simple

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"cochaviz/mime/internal/artifacts"
	"cochaviz/mime/internal/build"
	"cochaviz/mime/internal/build/adapters/libvirt"
	buildspecs "cochaviz/mime/internal/build/repositories"
	"cochaviz/mime/internal/logging"
	"cochaviz/mime/internal/sandbox/repositories/images"
	"cochaviz/mime/internal/setup"
)

var DefaultArtifactDir = filepath.Join(setup.StorageDir, "artifacts")
var DefaultImageDir = filepath.Join(setup.StorageDir, "images")
var DefaultBuildRoot = filepath.Join(setup.StorageDir, "builds")
var DefaultConnectionURI = "qemu:///system"

// Build executes the end-to-end flow to produce an image for the requested specification.
func Build(specificationID, imageDir, artifactDir, libvirtConnectionURI string) error {
	return BuildWithLogger(specificationID, imageDir, artifactDir, libvirtConnectionURI, nil)
}

// BuildWithLogger executes the end-to-end flow to produce an image for the requested specification using the provided logger.
func BuildWithLogger(specificationID, imageDir, artifactDir, libvirtConnectionURI string, logger *slog.Logger) error {
	logger = logging.Ensure(logger).With("component", "config.simple")

	specificationRepository := buildspecs.NewEmbeddedSpecificationRepository()

	if specificationID == "" {
		return fmt.Errorf("specification id is required")
	}
	if imageDir == "" {
		imageDir = DefaultImageDir
	}
	if artifactDir == "" {
		artifactDir = DefaultArtifactDir
	}

	if libvirtConnectionURI == "" {
		libvirtConnectionURI = DefaultConnectionURI
	}

	buildRoot := DefaultBuildRoot
	if err := os.MkdirAll(buildRoot, 0o755); err != nil {
		return fmt.Errorf("create build root %s: %w", buildRoot, err)
	}

	buildDir, err := os.MkdirTemp(buildRoot, "mime-build-*")
	if err != nil {
		return fmt.Errorf("create build directory: %w", err)
	}
	defer os.RemoveAll(buildDir)

	buildService := build.BuildService{
		Logger: logger.With("service", "build"),
		EnvironmentPreparer: &libvirt.LibvirtBuildEnvironmentPreparer{
			BaseDir:            buildDir,
			ConnectionURI:      libvirtConnectionURI,
			StoragePoolCleaner: libvirt.LibvirtStoragePoolCleaner{},
		},
		BuildDriver: &libvirt.VirtInstallBuilder{
			Logger: logger.With("driver", "virt-install"),
		},
		BuildSpecificationRepository: specificationRepository,
		ImageRepository: &images.LocalImageRepository{
			BaseDir: imageDir,
		},
		ArtifactStore: &artifacts.LocalArtifactStore{
			BaseDir: artifactDir,
		},
	}

	if err := buildService.Run(&build.BuildRequest{SpecificationID: specificationID}); err != nil {
		return err
	}

	return nil
}

// List prints the available specifications and whether an image exists locally.
func List(imageDir string) ([]string, []bool, error) {
	if imageDir == "" {
		imageDir = DefaultImageDir
	}

	imageRepository := &images.LocalImageRepository{BaseDir: imageDir}
	specificationRepository := buildspecs.NewEmbeddedSpecificationRepository()

	specifications, err := specificationRepository.ListAll()

	if err != nil {
		return nil, nil, err
	}

	built := make([]bool, len(specifications))
	specIDs := make([]string, len(specifications))

	for i, specification := range specifications {
		specIDs[i] = specification.ID
		latestImage, err := imageRepository.LatestForSpec(specification.ID)

		if err != nil {
			return nil, nil, err
		}
		built[i] = (latestImage != nil)
	}

	return specIDs, built, nil
}
