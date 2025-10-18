package simple

import (
	"fmt"
	"os"

	"cochaviz/mime/drivers/build/libvirt"
	"cochaviz/mime/models"
	embeddedrepositories "cochaviz/mime/repositories/embedded"
	localrepositories "cochaviz/mime/repositories/local"
	"cochaviz/mime/services"
	"cochaviz/mime/setup"
)

var DefaultArtifactDir = setup.StorageDir + "artifacts"
var DefaultImageDir = setup.StorageDir + "images"
var DefaultConnectionURI = "qemu:///system"

// Build executes the end-to-end flow to produce an image for the requested specification.
func Build(specificationID, imageDir, artifactDir, libvirtConnectionURI string) error {
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
		EnvironmentPreparer: &libvirt.LibvirtBuildEnvironmentPreparer{
			BaseDir:            buildDir,
			StoragePoolCleaner: libvirt.LibvirtStoragePoolCleaner{},
		},
		BuildDriver:                    &libvirt.VirtInstallBuilder{},
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

		fmt.Printf("%s: %t\n", specification.ID, latestImage != nil)
	}

	return nil
}
