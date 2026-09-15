package protocols

import (
	"context"

	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
)

// InstallAllRuntimes installs the runtime binaries for all registered protocol
// plugins plus the WARP (sing-box) runtime. It is the plugin-aware replacement
// for runtimeinstall.InstallAll.
func InstallAllRuntimes(ctx context.Context, opts runtimeinstall.Options) []runtimeinstall.Result {
	results, _ := installRuntimesFor(ctx, opts, NewRegistry(), nil)
	return results
}

// InstallSelectedRuntimes installs only the named runtime binaries. Names are
// protocol/runtime names such as naiveproxy, hysteria2, mieru, warp, and olcrtc.
// Unknown names are rejected before any install starts.
func InstallSelectedRuntimes(ctx context.Context, opts runtimeinstall.Options, only []string) ([]runtimeinstall.Result, error) {
	return installRuntimesFor(ctx, opts, NewRegistry(), only)
}

func installRuntimesFor(ctx context.Context, opts runtimeinstall.Options, r *Registry, only []string) ([]runtimeinstall.Result, error) {
	arch := opts.Arch
	if arch == "" {
		arch = "amd64"
	}

	runtimes := runtimeCatalogFor(arch, r)
	if len(only) > 0 {
		selected, err := runtimeinstall.FilterCatalog(runtimes, only)
		if err != nil {
			return nil, err
		}
		runtimes = selected
	}

	results := make([]runtimeinstall.Result, 0, len(runtimes))
	for _, r := range runtimes {
		results = append(results, runtimeinstall.Install(ctx, opts, r))
	}
	return results, nil
}

func runtimeCatalogFor(arch string, r *Registry) []runtimeinstall.Runtime {
	var runtimes []runtimeinstall.Runtime
	for _, p := range r.All() {
		rp, ok := AsRuntimeProvider(p)
		if !ok {
			continue
		}
		runtimes = append(runtimes, rp.RuntimeInstall(arch))
	}

	// Append non-plugin runtimes (e.g. WARP) from the runtimeinstall catalog.
	// The catalog is intentionally scoped to runtimes that are not already
	// contributed by protocol plugins.
	runtimes = append(runtimes, runtimeinstall.Catalog(arch)...)
	return runtimes
}
