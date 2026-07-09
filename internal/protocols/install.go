package protocols

import (
	"context"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
)

// InstallAllRuntimes installs the runtime binaries for all registered protocol
// plugins plus the WARP (sing-box) runtime. It is the plugin-aware replacement
// for runtimeinstall.InstallAll.
func InstallAllRuntimes(ctx context.Context, opts runtimeinstall.Options) []runtimeinstall.Result {
	return installRuntimesFor(ctx, opts, NewRegistry(), nil)
}

// InstallSelectedRuntimes installs only the named runtime binaries. Names are
// protocol/runtime names such as naiveproxy, hysteria2, mieru, warp, and olcrtc.
func InstallSelectedRuntimes(ctx context.Context, opts runtimeinstall.Options, only []string) []runtimeinstall.Result {
	return installRuntimesFor(ctx, opts, NewRegistry(), only)
}

func installRuntimesFor(ctx context.Context, opts runtimeinstall.Options, r *Registry, only []string) []runtimeinstall.Result {
	arch := opts.Arch
	if arch == "" {
		arch = "amd64"
	}

	runtimes := runtimeCatalogFor(arch, r)
	if len(only) > 0 {
		runtimes = filterRuntimeCatalog(runtimes, only)
	}

	results := make([]runtimeinstall.Result, 0, len(runtimes))
	for _, r := range runtimes {
		results = append(results, runtimeinstall.Install(ctx, opts, r))
	}
	return results
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

	// WARP is managed as a runtime but is not an inbound protocol plugin, so its
	// descriptor is still sourced from the runtimeinstall catalog.
	for _, r := range runtimeinstall.Catalog(arch) {
		if r.Name == "warp" {
			runtimes = append(runtimes, r)
			break
		}
	}
	return runtimes
}

func filterRuntimeCatalog(runtimes []runtimeinstall.Runtime, only []string) []runtimeinstall.Runtime {
	want := make(map[string]struct{}, len(only))
	for _, name := range only {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		want[name] = struct{}{}
	}
	filtered := make([]runtimeinstall.Runtime, 0, len(runtimes))
	for _, runtime := range runtimes {
		if _, ok := want[strings.ToLower(runtime.Name)]; ok {
			filtered = append(filtered, runtime)
		}
	}
	return filtered
}
