package build

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cochaviz/bottle/internal/artifacts"
	"cochaviz/bottle/internal/sandbox"
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

	setupArtifacts, err := storeLocalArtifacts(
		requestedSpec.SandboxSpecification.SetupFiles,
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

	// ImageID needs to be derived in order to link it to the reference specification
	imageID := deriveImageID(
		requestedSpec.SandboxSpecification.ID,
		requestedSpec.SandboxSpecification.Version,
	)

	refSpec := requestedSpec.SandboxSpecification
	if len(setupArtifacts) > 0 {
		refSpec.SetupFiles = cloneArtifactsList(setupArtifacts)
	} else {
		refSpec.SetupFiles = nil
	}

	image := sandbox.SandboxImage{
		ID:                     imageID,
		ReferenceSpecification: refSpec,
		ImageArtifact:          imageArtifact,
		CreatedAt:              time.Now(),
		Metadata:               map[string]any{},
		CompanionArtifacts:     append(companionArtifacts, setupArtifacts...),
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

func deriveImageID(specID, version string) string {
	specID = strings.TrimSpace(specID)
	version = strings.TrimSpace(version)
	timestamp := time.Now().UTC().Format("20060102-150405")

	switch {
	case specID != "" && version != "":
		return fmt.Sprintf("%s-%s-%s", specID, sanitizeID(version), timestamp)
	case specID != "":
		return fmt.Sprintf("%s-%s", specID, timestamp)
	default:
		return timestamp
	}
}

func sanitizeID(value string) string {
	return strings.NewReplacer(" ", "_", "/", "_", "\\", "_").Replace(value)
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

func cloneArtifactsList(items []artifacts.Artifact) []artifacts.Artifact {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]artifacts.Artifact, len(items))
	copy(cloned, items)
	return cloned
}
