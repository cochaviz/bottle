package build

import (
	"errors"
	"log/slog"
	"time"

	"cochaviz/mime/internal/artifacts"
	"cochaviz/mime/internal/sandbox"

	"github.com/google/uuid"
)

type BuildService struct {
	Logger                       *slog.Logger
	EnvironmentPreparer          BuildEnvironmentPreparer
	BuildDriver                  BuildDriver
	BuildSpecificationRepository BuildSpecificationRepository
	ImageRepository              sandbox.ImageRepository
	ArtifactStore                artifacts.ArtifactStore
}

func (s *BuildService) Run(request *BuildRequest) error {
	if s.BuildSpecificationRepository == nil {
		return errors.New("build specification repository is not configured")
	}

	logger := s.logger().With("specification", request.SpecificationID)

	requestedSpec, err := s.BuildSpecificationRepository.Get(request.SpecificationID)
	if err != nil {
		return err
	}

	logger = logger.With(
		"release", requestedSpec.Profile.Release,
		"architecture", requestedSpec.SandboxSpecification.DomainProfile.Arch,
	)
	logger.Info("starting sandbox build")

	context := BuildContext{
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

	imagePath, err := artifacts.PathFromURI(buildOutput.DiskImage.URI)
	if err != nil {
		return err
	}

	imageArtifact, err := s.ArtifactStore.StoreArtifact(
		imagePath,
		artifacts.ImageArtifact,
		map[string]any{},
	)
	if err != nil {
		return err
	}

	image := sandbox.SandboxImage{
		ID:                 uuid.New().String(),
		Specification:      requestedSpec.SandboxSpecification,
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

func storeLocalArtifacts(buildArtifacts []artifacts.Artifact, repository artifacts.ArtifactStore) ([]artifacts.Artifact, error) {
	storedArtifacts := []artifacts.Artifact{}

	for _, artifact := range buildArtifacts {
		path, err := artifacts.PathFromURI(artifact.URI)
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
