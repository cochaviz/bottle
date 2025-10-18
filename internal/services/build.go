package services

import (
	"log/slog"
	"time"

	"cochaviz/mime/internal/drivers/build"
	"cochaviz/mime/internal/models"
	"cochaviz/mime/internal/repositories"

	"github.com/google/uuid"
)

type BuildService struct {
	Logger                         *slog.Logger
	EnvironmentPreparer            build.BuildEnvironmentPreparer
	BuildDriver                    build.BuildDriver
	SandboxSpecificationRepository repositories.SandboxSpecficationRepository
	ImageRepository                repositories.ImageRepository
	ArtifactStore                  repositories.ArtifactStore
}

func (s *BuildService) Run(request *models.BuildRequest) error {
	logger := s.logger().With("specification", request.SpecificationID)

	requestedSpec, err := s.SandboxSpecificationRepository.Get(request.SpecificationID)
	if err != nil {
		return err
	}

	logger = logger.With(
		"release", requestedSpec.BuildProfile.Release,
		"architecture", requestedSpec.DomainProfile.Arch,
	)
	logger.Info("starting sandbox build")

	context := models.BuildContext{
		Spec:      requestedSpec,
		Overrides: request.ProfileOverrides,
	}

	env, err := s.EnvironmentPreparer.Prepare(context)
	if err != nil {
		return err
	}
	defer env.Cleanup(context)
	logger.Info("build environment prepared")

	buildOutput, err := s.BuildDriver.Build(context, env)
	if err != nil {
		return err
	}
	logger.Info("build driver completed", "disk_image", buildOutput.DiskImage.URI)

	companionArtifacts, err := storeLocalArtifacts(
		buildOutput.CompanionArtifacts,
		s.ArtifactStore,
	)
	if err != nil {
		return err
	}

	logger.Info("stored build artifacts",
		"companion_artifacts", len(companionArtifacts),
		"image_uri", buildOutput.DiskImage.URI,
	)

	imagePath, err := repositories.PathFromURI(buildOutput.DiskImage.URI)
	if err != nil {
		return err
	}

	imageArtifact, err := s.ArtifactStore.StoreArtifact(
		imagePath,
		models.ImageArtifact,
		map[string]any{},
	)
	if err != nil {
		return err
	}

	image := models.SandboxImage{
		ID:                 uuid.New().String(),
		Specification:      requestedSpec,
		Image:              imageArtifact,
		CreatedAt:          time.Now(),
		Metadata:           map[string]any{},
		CompanionArtifacts: companionArtifacts,
	}

	if err := s.ImageRepository.Save(image); err != nil {
		return err
	}

	logger.Info("sandbox image saved", "image_id", image.ID)
	return nil
}

func (s BuildService) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
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
