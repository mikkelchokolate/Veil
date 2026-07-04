package protocols

import (
	"context"

	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
)

// InstallAllRuntimes installs the runtime binaries for all registered protocol
// plugins plus the WARP (sing-box) runtime. It is the plugin-aware replacement
// for runtimeinstall.InstallAll.
func InstallAllRuntimes(ctx context.Context, opts runtimeinstall.Options) []runtimeinstall.Result {
	arch := opts.Arch
	if arch == "" {
		arch = "amd64"
	}

	var runtimes []runtimeinstall.Runtime
	for _, p := range NewRegistry().All() {
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

	results := make([]runtimeinstall.Result, 0, len(runtimes))
	for _, r := range runtimes {
		results = append(results, runtimeinstall.Install(ctx, opts, r))
	}
	return results
}
