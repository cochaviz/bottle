package configurations

import (
	"cochaviz/mime/drivers/build/libvirt"
	"cochaviz/mime/models"
	embedded_repostories "cochaviz/mime/repositories/embedded"
	local_repostories "cochaviz/mime/repositories/local"
	"cochaviz/mime/services"
	"fmt"
	"os"
)

// Simple end-to-end function for building a simple image
func build(
	specificationID string,
	imageDir string,
	artifactDir string,
	libvirtConnectionURI string,
) {
	if libvirtConnectionURI == "" {
		libvirtConnectionURI = "qemu:///system"
	}
	buildDir := os.TempDir()

	buildService := services.BuildService{
		EnvironmentPreparer: &libvirt.LibvirtBuildEnvironmentPreparer{
			BaseDir:            buildDir,
			StoragePoolCleaner: libvirt.LibvirtStoragePoolCleaner{},
			ConnectionURI:      libvirtConnectionURI,
		},
		BuildDriver:                    &libvirt.VirtInstallBuilder{},
		SandboxSpecificationRepository: &embedded_repostories.EmbeddedSpecificationRepository{},
		ImageRepository: &local_repostories.LocalImageRepository{
			BaseDir: imageDir,
		},
		ArtifactStore: &local_repostories.LocalArtifactStore{
			BaseDir: artifactDir,
		},
	}

	err := buildService.Run(
		&models.BuildRequest{
			SpecificationID: specificationID,
		},
	)
	if err != nil {
		panic(err)
	}
}

// Lists all available specifications and whether the are built
func list(
	imageDir string,
) {
	imageRepository := &local_repostories.LocalImageRepository{
		BaseDir: imageDir,
	}
	specificationRepository := embedded_repostories.EmbeddedSpecificationRepository{}

	specifications, err := specificationRepository.ListAll()
	if err != nil {
		panic(err)
	}

	for _, specification := range specifications {
		latestImage, err := imageRepository.LatestForSpec(specification.ID)
		if err != nil {
			panic(err)
		}

		fmt.Printf("%s: %t\n", specification.ID, latestImage != nil)
	}
}
