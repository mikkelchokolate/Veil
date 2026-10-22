package privileged

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
)

type RuntimeInstallRequest struct {
	Names []string   `json:"names"`
	Fence FenceToken `json:"fence"`
}

type RuntimeInstallStatusRequest struct {
	OperationID string `json:"operationId"`
}

type RuntimeInstallStatusResult struct {
	OperationID string `json:"operationId"`
	Stage       string `json:"stage"`
	Runtime     string `json:"runtime,omitempty"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// shortcut: retains one operation, persist records if installs become concurrent.
type runtimeInstallProgress struct {
	mu   sync.Mutex
	last RuntimeInstallStatusResult
	now  func() time.Time
}

func (p *runtimeInstallProgress) start(operationID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = RuntimeInstallStatusResult{OperationID: operationID, Stage: "checking", UpdatedAt: p.now().UTC().Unix()}
}

func (p *runtimeInstallProgress) update(operationID, stage, name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last.OperationID != operationID {
		return
	}
	p.last.Stage, p.last.Runtime, p.last.UpdatedAt = stage, name, p.now().UTC().Unix()
}

func (p *runtimeInstallProgress) status(operationID string) RuntimeInstallStatusResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last.OperationID != operationID {
		return RuntimeInstallStatusResult{}
	}
	return p.last
}

func validateRuntimeInstallRequest(request RuntimeInstallRequest) error {
	if len(request.Names) == 0 || len(request.Names) > 5 {
		return newError(ErrorInvalidRequest, "runtime install requires 1-5 runtime names")
	}
	seen := make(map[string]bool, len(request.Names))
	for _, name := range request.Names {
		switch name {
		case "naiveproxy", "hysteria2", "mieru", "olcrtc", "warp":
		default:
			return newError(ErrorForbiddenOperation, "runtime name is not allowed")
		}
		if seen[name] {
			return newError(ErrorInvalidRequest, "runtime names must be unique")
		}
		seen[name] = true
	}
	return nil
}

func (a *LocalAdapter) InstallRuntimes(ctx context.Context, request RuntimeInstallRequest) error {
	if err := validateRuntimeInstallRequest(request); err != nil {
		return err
	}
	if err := a.fence.Accept(request.Fence); err != nil {
		return err
	}
	if a.executor.InstallRuntimes == nil {
		return newError(ErrorOperationFailed, "runtime installer executor is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, mutationOperationBudget)
	defer cancel()
	return wrapOperationError(a.executor.InstallRuntimes(ctx, request))
}

func (a *LocalAdapter) RuntimeInstallStatus(ctx context.Context, request RuntimeInstallStatusRequest) (RuntimeInstallStatusResult, error) {
	if strings.TrimSpace(request.OperationID) == "" || len(request.OperationID) > 256 {
		return RuntimeInstallStatusResult{}, newError(ErrorInvalidRequest, "runtime install operation ID is required and must be at most 256 bytes")
	}
	if a.executor.RuntimeInstallStatus == nil {
		return RuntimeInstallStatusResult{}, newError(ErrorOperationFailed, "runtime install status executor is unavailable")
	}
	return a.executor.RuntimeInstallStatus(ctx, request)
}

func runProductionRuntimeInstall(ctx context.Context, config ProductionConfig, request RuntimeInstallRequest, progress func(string, string)) error {
	if err := validateRuntimeInstallRequest(request); err != nil {
		return err
	}
	arch, err := hostenv.NormalizeArch(hostenv.CurrentPlatform().Arch)
	if err != nil {
		return err
	}
	catalog := runtimeinstall.Catalog(arch)
	for _, plugin := range protocols.NewRegistry().All() {
		if provider, ok := protocols.AsRuntimeProvider(plugin); ok {
			catalog = append(catalog, provider.RuntimeInstall(arch))
		}
	}
	selected, err := runtimeinstall.FilterCatalog(catalog, request.Names)
	if err != nil {
		return err
	}
	// The helper cannot access /root (ProtectHome=yes); keep builds in the
	// root-owned etc tree, outside the service-writable state directory.
	buildHome := filepath.Join(filepath.Dir(config.IPCertDir), ".runtime-build")
	for _, runtime := range selected {
		if runtime.Method == runtimeinstall.MethodGoInstall || runtime.Method == runtimeinstall.MethodCaddyNaive {
			if err := os.MkdirAll(buildHome, 0o700); err != nil {
				return err
			}
			dir, err := openHelperOwnedDir("runtime build home", buildHome, uint32(os.Geteuid()))
			if err != nil {
				return err
			}
			defer dir.Close()
			break
		}
	}
	ctx = runtimeinstall.ContextWithBuildHome(ctx, buildHome)
	opts := runtimeinstall.Options{BinDir: runtimeinstall.DefaultBinDir(), Arch: arch, CaddyCacheDir: buildHome, Progress: progress}
	for _, runtime := range selected {
		result := config.InstallRuntime(ctx, opts, runtime)
		if result.Err != nil {
			return fmt.Errorf("install %s runtime: %w", runtime.Name, result.Err)
		}
		if !result.Installed {
			return fmt.Errorf("install %s runtime: %s", runtime.Name, result.SkipReason)
		}
	}
	return nil
}

var _ RuntimeInstaller = (*LocalAdapter)(nil)
var _ RuntimeInstallObserver = (*LocalAdapter)(nil)
