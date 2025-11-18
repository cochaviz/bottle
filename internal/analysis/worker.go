package analysis

import (
	"cochaviz/mime/internal/sandbox"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	sampleExecutionTimeout = 2 * time.Minute
	sandboxLifetime        = 5 * time.Minute
	instrumentationWarmup  = 5 * time.Second
	postSampleDelay        = 5 * time.Second
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

	c2Ip            string
	archOverride    string
	sample          Sample
	sampleArgs      []string
	instrumentation []Instrumentation
}

func NewAnalysisWorker(
	logger *slog.Logger,
	driver sandbox.SandboxDriver,
	imageRepo sandbox.ImageRepository,
	c2Ip string,
	archOverride string,
	sample Sample,
	sampleArgs []string,
	instrumentation []Instrumentation,
) *AnalysisWorker {
	return &AnalysisWorker{
		logger:          logger,
		driver:          driver,
		imageRepo:       imageRepo,
		c2Ip:            c2Ip,
		archOverride:    strings.TrimSpace(archOverride),
		sample:          sample,
		sampleArgs:      append([]string(nil), sampleArgs...),
		instrumentation: append([]Instrumentation(nil), instrumentation...),
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
	if len(sandboxImages) == 0 {
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

	releaseLease := func() {
		if err := w.driver.Release(lease, true); err != nil {
			w.logger.Error("failed to release sandbox lease", "error", err)
		}
	}

	sandboxWorker := sandbox.NewSandboxWorker(w.driver, lease, w.logger)

	startCh := make(chan struct{})
	if len(w.instrumentation) > 0 {
		sandboxWorker.SetStartNotifier(startCh)
	}

	analysisCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	workerErr := make(chan error, 1)
	go func() {
		workerErr <- sandboxWorker.Run(analysisCtx)
	}()

	if len(w.instrumentation) > 0 {
		select {
		case <-startCh:
		case err := <-workerErr:
			return fmt.Errorf("sandbox worker failed: %w", err)
		case <-analysisCtx.Done():
			return analysisCtx.Err()
		}

		instCleanup, err := w.startInstrumentation(analysisCtx, lease)
		if err != nil {
			releaseLease()
			return err
		}
		if instCleanup != nil {
			defer instCleanup()
			w.logger.Info("instrumentation started", "count", len(w.instrumentation))
		}

		waitWithContext(analysisCtx, instrumentationWarmup, func() {
			w.logger.Info("instrumentation warm-up complete")
		})
	}

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

	stopOnce := sync.Once{}
	requestStop := func(reason string) {
		stopOnce.Do(func() {
			w.logger.Info(reason)
			stopResp := make(chan sandbox.SandboxWorkerSignalResponse, 1)
			worker.SignalChannel() <- sandbox.SandboxWorkerSignal{
				Type:     sandbox.SandboxWorkerSignalStop,
				Response: stopResp,
			}
			go func() {
				select {
				case <-stopResp:
					w.logger.Info("sandbox worker stopped")
				case <-time.After(30 * time.Second):
					w.logger.Warn("timeout waiting for sandbox worker stop response")
				case <-ctx.Done():
				}
			}()
		})
	}

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
			} else if resp.Result != nil {
				w.logger.Info("sample execution finished", "exit_code", resp.Result.ExitCode)
			}
			waitWithContext(ctx, postSampleDelay, func() {
				w.logger.Info("post-sample delay complete, stopping sandbox")
			})
			requestStop("sample execution completed, stopping sandbox")
		case <-ctx.Done():
		}
	}()

	go func() {
		timer := time.NewTimer(sandboxLifetime)
		defer timer.Stop()

		select {
		case <-timer.C:
			requestStop("sandbox lifetime reached, stopping worker")
		case <-ctx.Done():
			return
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

func (w *AnalysisWorker) instrumentationVariables(ctx context.Context, lease sandbox.SandboxLease) ([]InstrumentationVariable, error) {
	vmIP, err := leaseVMIP(lease)
	if err != nil {
		return nil, err
	}
	vmInterface, err := leaseVMInterface(lease)
	if err != nil {
		return nil, err
	}
	var vars []InstrumentationVariable
	vars = append(vars, InstrumentationVariable{Name: InstrumentationSampleName, Value: w.sample.Name})
	vars = append(vars, InstrumentationVariable{Name: InstrumentationVMInterface, Value: vmInterface})
	vars = append(vars, InstrumentationVariable{Name: InstrumentationVMIP, Value: vmIP})
	if ip := strings.TrimSpace(w.c2Ip); ip != "" {
		vars = append(vars, InstrumentationVariable{Name: InstrumentationC2Address, Value: ip})
	}
	return vars, nil
}

func (w *AnalysisWorker) startInstrumentation(ctx context.Context, lease sandbox.SandboxLease) (func(), error) {
	if len(w.instrumentation) == 0 {
		return nil, nil
	}
	vars, err := w.instrumentationVariables(ctx, lease)
	if err != nil {
		return nil, err
	}
	for _, inst := range w.instrumentation {
		if inst == nil {
			continue
		}
		if err := inst.Start(ctx, lease, vars...); err != nil {
			return nil, fmt.Errorf("start instrumentation: %w", err)
		}
	}
	cleanup := func() {
		for _, inst := range w.instrumentation {
			if inst == nil {
				continue
			}
			if err := inst.Close(); err != nil {
				w.logger.Warn("instrumentation close failed", "error", err)
			}
		}
	}
	return cleanup, nil
}

func leaseVMInterface(lease sandbox.SandboxLease) (string, error) {
	if lease.Metadata == nil {
		return "", errors.New("sandbox lease metadata missing vm_interface")
	}
	value, ok := lease.Metadata["vm_interface"]
	if !ok {
		return "", errors.New("sandbox lease missing vm_interface metadata")
	}
	iface, ok := value.(string)
	if !ok {
		return "", errors.New("sandbox lease vm_interface metadata must be a string")
	}
	iface = strings.TrimSpace(iface)
	if iface == "" {
		return "", errors.New("sandbox lease vm_interface metadata is empty")
	}
	return iface, nil
}

func waitWithContext(ctx context.Context, d time.Duration, onComplete func()) {
	if d <= 0 {
		if onComplete != nil {
			onComplete()
		}
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		if onComplete != nil {
			onComplete()
		}
	case <-ctx.Done():
	}
}
