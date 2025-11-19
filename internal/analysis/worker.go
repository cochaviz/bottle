package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cochaviz/bottle/internal/sandbox"
)

const (
	DefaultSampleExecutionTimeout time.Duration = 0
	DefaultSandboxLifetime        time.Duration = 0

	instrumentationWarmup          = 5 * time.Second
	postSampleDelay                = 5 * time.Second
	instrumentationStartTimeFormat = "20060102T150405Z"
	instrumentationLogRoot         = "/var/log/bottle"
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

	c2Ip                   string
	archOverride           string
	sample                 Sample
	sampleArgs             []string
	instrumentation        []Instrumentation
	sampleExecutionTimeout time.Duration
	sandboxLifetime        time.Duration
	logDir                 string
	configWritten          bool
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
	sampleExecutionTimeout time.Duration,
	sandboxLifetime time.Duration,
) *AnalysisWorker {
	if sampleExecutionTimeout < 0 {
		sampleExecutionTimeout = 0
	}
	if sandboxLifetime < 0 {
		sandboxLifetime = 0
	}
	return &AnalysisWorker{
		logger:                 logger,
		driver:                 driver,
		imageRepo:              imageRepo,
		c2Ip:                   c2Ip,
		archOverride:           strings.TrimSpace(archOverride),
		sample:                 sample,
		sampleArgs:             append([]string(nil), sampleArgs...),
		instrumentation:        append([]Instrumentation(nil), instrumentation...),
		sampleExecutionTimeout: sampleExecutionTimeout,
		sandboxLifetime:        sandboxLifetime,
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

	var startedInstrumentation []Instrumentation
	if len(w.instrumentation) > 0 {
		select {
		case <-startCh:
			lease.StartTime = time.Now().UTC()
		case err := <-workerErr:
			return fmt.Errorf("sandbox worker failed: %w", err)
		case <-analysisCtx.Done():
			return analysisCtx.Err()
		}

		var instCleanup func()
		var err error
		startedInstrumentation, instCleanup, err = w.startInstrumentation(analysisCtx, &lease)
		if err != nil {
			releaseLease()
			return err
		}
		if instCleanup != nil {
			defer instCleanup()
		}
		if len(startedInstrumentation) > 0 {
			w.logger.Info("instrumentation started", "count", len(startedInstrumentation))

			waitWithContext(analysisCtx, instrumentationWarmup, func() {
				w.logger.Info("instrumentation warm-up complete")
			})

			if err := w.ensureInstrumentationRunning(startedInstrumentation); err != nil {
				releaseLease()
				return err
			}
		} else {
			w.logger.Info("no instrumentation started after applying requirements")
		}
	}

	if _, err := w.ensureLogDir(&lease); err != nil {
		w.logger.Warn("failed to prepare log directory", "error", err)
	} else {
		w.writeAnalysisConfig(lease)
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
			Timeout: w.sampleExecutionTimeout,
		},
		Response: execResp,
	}

	go func() {
		select {
		case resp := <-execResp:
			if resp.Err != nil {
				if errors.Is(resp.Err, sandbox.ErrGuestCommandTimedOut) {
					w.logger.Warn("sample execution timed out", "timeout", w.sampleExecutionTimeout)
				} else {
					w.logger.Error("sample execution failed", "error", resp.Err)
				}
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

	if w.sandboxLifetime > 0 {
		go func() {
			timer := time.NewTimer(w.sandboxLifetime)
			defer timer.Stop()

			select {
			case <-timer.C:
				requestStop("sandbox lifetime reached, stopping worker")
			case <-ctx.Done():
				return
			}
		}()
	}
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

func (w *AnalysisWorker) instrumentationVariables(lease *sandbox.SandboxLease) ([]InstrumentationVariable, error) {
	vmIP, err := leaseVMIP(*lease)
	if err != nil {
		return nil, err
	}
	vmInterface, err := leaseVMInterface(*lease)
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
	start := lease.StartTime
	if start.IsZero() {
		start = time.Now().UTC()
		lease.StartTime = start
	}
	startValue := start.Format(instrumentationStartTimeFormat)
	vars = append(vars, InstrumentationVariable{
		Name:  InstrumentationStartTime,
		Value: startValue,
	})
	if runDir := strings.TrimSpace(lease.RunDir); runDir != "" {
		vars = append(vars, InstrumentationVariable{Name: InstrumentationRunDir, Value: runDir})
	}
	logDir, err := w.ensureLogDir(lease)
	if err != nil {
		return nil, err
	}
	vars = append(vars, InstrumentationVariable{Name: InstrumentationLogDir, Value: logDir})
	return vars, nil
}

func (w *AnalysisWorker) startInstrumentation(ctx context.Context, lease *sandbox.SandboxLease) ([]Instrumentation, func(), error) {
	if len(w.instrumentation) == 0 {
		return nil, nil, nil
	}
	vars, err := w.instrumentationVariables(lease)
	if err != nil {
		return nil, nil, err
	}
	var started []Instrumentation
	for _, inst := range w.instrumentation {
		if inst == nil {
			continue
		}
		if err := inst.Start(ctx, *lease, vars...); err != nil {
			var missingErr *MissingRequiredVariablesError
			if errors.As(err, &missingErr) {
				w.logger.Info("skipping instrumentation due to missing variables",
					"instrumentation", inst.Name(), "missing", strings.Join(missingErr.Missing, ", "))
				continue
			}
			return nil, nil, fmt.Errorf("start instrumentation %q: %w", inst.Name(), err)
		}
		started = append(started, inst)
	}
	if len(started) == 0 {
		return nil, nil, nil
	}
	cleanup := func() {
		for _, inst := range started {
			if inst == nil {
				continue
			}
			if err := inst.Close(); err != nil {
				w.logger.Warn("instrumentation close failed", "instrumentation", inst.Name(), "error", err)
			}
		}
	}
	return started, cleanup, nil
}

func (w *AnalysisWorker) ensureInstrumentationRunning(active []Instrumentation) error {
	if len(active) == 0 {
		return nil
	}
	var failures []string
	for _, inst := range active {
		if inst == nil {
			continue
		}
		if err := inst.Running(); err != nil {
			name := strings.TrimSpace(inst.Name())
			if name == "" {
				name = "instrumentation"
			}
			failures = append(failures, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("instrumentation not running after warm-up: %s", strings.Join(failures, "; "))
	}
	return nil
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

func (w *AnalysisWorker) ensureLogDir(lease *sandbox.SandboxLease) (string, error) {
	if s := strings.TrimSpace(w.logDir); s != "" {
		return s, nil
	}
	start := lease.StartTime
	if start.IsZero() {
		start = time.Now().UTC()
		lease.StartTime = start
	}
	name := strings.TrimSpace(w.sample.Name)
	if name == "" {
		name = "sample"
	}
	dirName := fmt.Sprintf("%s-%s", name, start.Format(instrumentationStartTimeFormat))
	logDir := filepath.Join(instrumentationLogRoot, dirName)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", fmt.Errorf("create instrumentation log dir %s: %w", logDir, err)
	}
	w.logDir = logDir
	return logDir, nil
}

func (w *AnalysisWorker) writeAnalysisConfig(lease sandbox.SandboxLease) {
	if strings.TrimSpace(w.logDir) == "" || w.configWritten {
		return
	}
	cfg := struct {
		SampleName             string        `json:"sample_name"`
		SamplePath             string        `json:"sample_path"`
		SampleArgs             []string      `json:"sample_args"`
		C2Address              string        `json:"c2_address,omitempty"`
		ArchOverride           string        `json:"arch_override,omitempty"`
		StartTime              time.Time     `json:"start_time"`
		RunDir                 string        `json:"run_dir"`
		LogDir                 string        `json:"log_dir"`
		SampleExecutionTimeout time.Duration `json:"sample_execution_timeout"`
		SandboxLifetime        time.Duration `json:"sandbox_lifetime"`
		InstrumentationCount   int           `json:"instrumentation_count"`
	}{
		SampleName:             w.sample.Name,
		SamplePath:             w.sample.Artifact,
		SampleArgs:             append([]string(nil), w.sampleArgs...),
		C2Address:              strings.TrimSpace(w.c2Ip),
		ArchOverride:           strings.TrimSpace(w.archOverride),
		StartTime:              lease.StartTime,
		RunDir:                 lease.RunDir,
		LogDir:                 w.logDir,
		SampleExecutionTimeout: w.sampleExecutionTimeout,
		SandboxLifetime:        w.sandboxLifetime,
		InstrumentationCount:   len(w.instrumentation),
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		w.logger.Warn("failed to marshal analysis config", "error", err)
		return
	}
	path := filepath.Join(w.logDir, "analysis-config.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		w.logger.Warn("failed to write analysis config", "error", err)
		return
	}
	w.configWritten = true
}
