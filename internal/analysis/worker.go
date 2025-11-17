package analysis

import (
	"cochaviz/mime/internal/sandbox"
	"context"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	sampleExecutionTimeout = 2 * time.Minute
	sandboxLifetime        = 5 * time.Minute
)

type Sample struct {
	ID       string
	Name     string
	Artifact string
}

type AnalysisWorker struct {
	logger *slog.Logger

	driver sandbox.SandboxDriver

	imageRepo sandbox.ImageRepository

	c2Ip         string
	archOverride string
	sample       Sample
	sampleArgs   []string
}

func NewAnalysisWorker(
	logger *slog.Logger,
	driver sandbox.SandboxDriver,
	imageRepo sandbox.ImageRepository,
	c2Ip string,
	archOverride string,
	sample Sample,
	sampleArgs []string,
) *AnalysisWorker {
	return &AnalysisWorker{
		logger:       logger,
		driver:       driver,
		imageRepo:    imageRepo,
		c2Ip:         c2Ip,
		archOverride: strings.TrimSpace(archOverride),
		sample:       sample,
		sampleArgs:   append([]string(nil), sampleArgs...),
	}
}

func (w *AnalysisWorker) Run(ctx context.Context) error {
	arch := w.archOverride
	if arch == "" {
		predicted_arch, err := determineSampleArchitecture(w.sample)

		if err != nil {
			return fmt.Errorf("unable to determine architecture for %q: %w", w.sample.Name, err)
		}
		arch = predicted_arch
	}

	sandboxImages, err := w.imageRepo.FilterByArchitecture(arch)
	if err != nil {
		return fmt.Errorf("filter sandbox image by architecture: %w", err)
	}
	if sandboxImages == nil {
		return fmt.Errorf("no sandbox image found for architecture %q", arch)
	}

	sampleDir := filepath.Dir(w.sample.Artifact)

	lease, err := w.driver.Acquire(sandbox.SandboxLeaseSpecification{
		DomainName:   fmt.Sprintf("sandbox-%s", w.sample.Name),
		SampleDir:    sampleDir,
		SandboxImage: *sandboxImages[0], // we just take the first image for simplicity
	})
	if err != nil {
		return fmt.Errorf("acquire sandbox lease: %w", err)
	}

	cleanupWhitelist, err := w.configureC2Whitelist(lease)
	if err != nil {
		if releaseErr := w.driver.Release(lease, true); releaseErr != nil {
			w.logger.Error("failed to release sandbox after whitelist error", "error", releaseErr)
		}
		return err
	}
	if cleanupWhitelist != nil {
		defer cleanupWhitelist()
	}

	sandboxWorker := sandbox.NewSandboxWorker(w.driver, lease, w.logger)

	analysisCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	workerErr := make(chan error, 1)
	go func() {
		workerErr <- sandboxWorker.Run(analysisCtx)
	}()

	w.dispatchSampleExecution(analysisCtx, sandboxWorker, sampleDir)

	if err := <-workerErr; err != nil {
		return fmt.Errorf("sandbox worker failed: %w", err)
	}

	return nil
}

func (w *AnalysisWorker) dispatchSampleExecution(ctx context.Context, worker *sandbox.SandboxWorker, sampleDir string) {
	relativePath, err := filepath.Rel(sampleDir, w.sample.Artifact)
	if err != nil {
		relativePath = filepath.Base(w.sample.Artifact)
	}
	relativePath = filepath.ToSlash(relativePath)
	guestSamplePath := path.Join(sandbox.GuestSampleMountPath, relativePath)

	execResp := make(chan sandbox.SandboxWorkerSignalResponse, 1)
	worker.SignalChannel() <- sandbox.SandboxWorkerSignal{
		Type: sandbox.SandboxWorkerSignalExecuteCommand,
		Command: &sandbox.SandboxCommand{
			Path:    guestSamplePath,
			Args:    append([]string(nil), w.sampleArgs...),
			Timeout: sampleExecutionTimeout,
		},
		Response: execResp,
	}

	go func() {
		select {
		case resp := <-execResp:
			if resp.Err != nil {
				w.logger.Error("sample execution failed", "error", resp.Err)
				return
			}
			if resp.Result != nil {
				w.logger.Info("sample execution finished", "exit_code", resp.Result.ExitCode)
			}
		case <-ctx.Done():
		}
	}()

	go func() {
		timer := time.NewTimer(sandboxLifetime)
		defer timer.Stop()

		select {
		case <-timer.C:
			w.logger.Info("sandbox lifetime reached, stopping worker")
		case <-ctx.Done():
			return
		}

		stopResp := make(chan sandbox.SandboxWorkerSignalResponse, 1)
		worker.SignalChannel() <- sandbox.SandboxWorkerSignal{
			Type:     sandbox.SandboxWorkerSignalStop,
			Response: stopResp,
		}

		select {
		case <-stopResp:
			w.logger.Info("sandbox worker stopped")
		case <-time.After(30 * time.Second):
			w.logger.Warn("timeout waiting for sandbox worker stop response")
		case <-ctx.Done():
		}
	}()
}

func (w *AnalysisWorker) configureC2Whitelist(lease sandbox.SandboxLease) (func(), error) {
	ip := strings.TrimSpace(w.c2Ip)
	if ip == "" {
		return nil, nil
	}

	cleanup, err := WhitelistIP(lease, ip)
	if err != nil {
		return nil, fmt.Errorf("whitelist C2 IP: %w", err)
	}

	return func() {
		if err := cleanup(); err != nil {
			w.logger.Error("failed to remove C2 whitelist", "error", err)
		}
	}, nil
}
