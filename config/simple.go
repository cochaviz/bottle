package simple

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"cochaviz/mime/internal/analysis"
	"cochaviz/mime/internal/artifacts"
	"cochaviz/mime/internal/build"
	"cochaviz/mime/internal/build/adapters/libvirt"
	buildspecs "cochaviz/mime/internal/build/repositories"
	"cochaviz/mime/internal/logging"
	"cochaviz/mime/internal/sandbox"
	"cochaviz/mime/internal/sandbox/repositories/images"
	"cochaviz/mime/internal/setup"

	"github.com/google/uuid"
)

var DefaultArtifactDir = filepath.Join(setup.StorageDir, "artifacts")
var DefaultImageDir = filepath.Join(setup.StorageDir, "images")
var DefaultBuildRoot = filepath.Join(setup.StorageDir, "builds")
var DefaultRunDir = filepath.Join(setup.StorageDir, "leases")
var DefaultSpecificationDir = filepath.Join(setup.StorageDir, "specifications")
var DefaultConnectionURI = "qemu:///system"

// BuildSandbox executes the end-to-end flow to produce an image for the requested specification using the provided logger.
func BuildSandbox(specificationID, imageDir, artifactDir, libvirtConnectionURI string, logger *slog.Logger) error {
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

// RunSandbox acquires and runs a sandbox until the provided context is cancelled or the domain stops.
func RunSandbox(
	ctx context.Context,
	specificationID,
	imageDir,
	runDir,
	sampleDir,
	domainName,
	libvirtConnectionURI string,
	logger *slog.Logger,
) error {
	logger = logging.Ensure(logger).With("component", "config.simple", "operation", "run")

	if specificationID == "" {
		return fmt.Errorf("specification id is required")
	}
	if imageDir == "" {
		imageDir = DefaultImageDir
	}
	if runDir == "" {
		runDir = DefaultRunDir
	}
	if libvirtConnectionURI == "" {
		libvirtConnectionURI = DefaultConnectionURI
	}

	imageRepository := &images.LocalImageRepository{BaseDir: imageDir}
	image, err := imageRepository.LatestForSpec(specificationID)
	if err != nil {
		return fmt.Errorf("lookup image: %w", err)
	}
	if image == nil {
		return fmt.Errorf("no image found for specification %s in %s", specificationID, imageDir)
	}

	driver := &sandbox.LibvirtDriver{
		ConnectionURI: libvirtConnectionURI,
		BaseDir:       runDir,
		Logger:        logger.With("driver", "libvirt"),
	}

	lease, err := driver.Acquire(sandbox.SandboxLeaseSpecification{
		DomainName:   domainName,
		SampleDir:    sampleDir,
		SandboxImage: *image,
	})
	if err != nil {
		return fmt.Errorf("acquire sandbox: %w", err)
	}

	worker := sandbox.NewSandboxWorker(driver, lease, logger.With("worker", lease.ID))
	logger.Info("sandbox lease acquired", "lease_id", lease.ID)

	return worker.Run(ctx)
}

// RunAnalysis executes the analysis workflow for the provided sample file.
func RunAnalysis(
	ctx context.Context,
	samplePath string,
	c2Address string,
	imageDir string,
	runDir string,
	libvirtConnectionURI string,
	overrideArch string,
	sampleArgs []string,
	instrumentations []analysis.Instrumentation,
	logger *slog.Logger,
) error {
	logger = logging.Ensure(logger).With("component", "config.simple", "operation", "analysis")

	if strings.TrimSpace(samplePath) == "" {
		return fmt.Errorf("sample path is required")
	}

	info, err := os.Stat(samplePath)
	if err != nil {
		return fmt.Errorf("stat sample: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("sample path %s is a directory; provide a file", samplePath)
	}
	absSample, err := filepath.Abs(samplePath)
	if err != nil {
		return fmt.Errorf("resolve sample path: %w", err)
	}

	if imageDir == "" {
		imageDir = DefaultImageDir
	}
	if runDir == "" {
		runDir = DefaultRunDir
	}
	if libvirtConnectionURI == "" {
		libvirtConnectionURI = DefaultConnectionURI
	}

	imageRepository := &images.LocalImageRepository{BaseDir: imageDir}
	driver := &sandbox.LibvirtDriver{
		ConnectionURI: libvirtConnectionURI,
		BaseDir:       runDir,
		Logger:        logger.With("driver", "libvirt"),
	}

	sampleName := filepath.Base(absSample)
	sample := analysis.Sample{
		ID:       fmt.Sprintf("%s-%s", sampleName, uuid.NewString()),
		Name:     sampleName,
		Artifact: absSample,
	}

	worker := analysis.NewAnalysisWorker(
		logger.With("worker", sample.ID),
		driver,
		imageRepository,
		c2Address,
		overrideArch,
		sample,
		sampleArgs,
		instrumentations,
	)

	logger.Info("starting analysis worker", "sample", sample.Name, "c2", c2Address)
	if err := worker.Run(ctx); err != nil {
		return err
	}
	logger.Info("analysis worker completed", "sample", sample.Name)
	return nil
}
