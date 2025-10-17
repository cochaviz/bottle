package services

import (
	"cochaviz/mime/drivers/build"
	"cochaviz/mime/models"
	"cochaviz/mime/repositories"
	"time"

	"github.com/google/uuid"
)

type BuildService struct {
	EnvironmentPreparer            build.BuildEnvironmentPreparer
	BuildDriver                    build.BuildDriver
	SandboxSpecificationRepository repositories.SandboxSpecficiationRepository
	ImageRepository                repositories.ImageRepository
	ArtifactStore                  repositories.ArtifactStore
}

func (service BuildService) Run(request *models.BuildRequest) error {
	requestedSpec, err := service.SandboxSpecificationRepository.Get(request.SpecificationID)
	if err != nil {
		return err
	}

	context := models.BuildContext{
		Spec:      requestedSpec,
		Overrides: request.ProfileOverrides,
	}

	env, err := service.EnvironmentPreparer.Prepare(context)
	if err != nil {
		return err
	}
	defer env.Cleanup(context)

	buildOutput, err := service.BuildDriver.Build(context, env)
	if err != nil {
		return err
	}

	companionArtifacts, err := storeLocalArtifacts(
		buildOutput.CompanionArtifacts,
		service.ArtifactStore,
	)
	if err != nil {
		return err
	}

	imagePath, err := repositories.PathFromURI(buildOutput.DiskImage.URI)
	if err != nil {
		return err
	}

	imageArtifact, err := service.ArtifactStore.StoreArtifact(
		imagePath,
		models.ImageArtifact,
		map[string]any{},
	)
	if err != nil {
		return err
	}

	// Perhaps it would be better to use a transactional approach here
	image := models.SandboxImage{
		ID:                 uuid.New().String(),
		Specification:      requestedSpec,
		Image:              imageArtifact,
		CreatedAt:          time.Now(),
		Metadata:           map[string]any{},
		CompanionArtifacts: companionArtifacts,
	}

	// Lastly, save the image to the repository
	return service.ImageRepository.Save(image)
}

func storeLocalArtifacts(artifacts []models.Artifact, repository repositories.ArtifactStore) ([]models.Artifact, error) {
	storedArtifacts := []models.Artifact{}

	for _, artifact := range artifacts {
		path, err := repositories.PathFromURI(artifact.URI)

		if err != nil {
			return nil, err
		}
		storedArtifact, err := repository.StoreArtifact(
			path,
			artifact.Kind,
			artifact.Metadata,
		)
		if err != nil {
			return nil, err
		}
		storedArtifacts = append(storedArtifacts, storedArtifact)
	}
	return storedArtifacts, nil
}
