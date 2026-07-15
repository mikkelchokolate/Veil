package protocols

import (
	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
)

// ManagedProcessPolicy returns a process-discovery policy that recognizes the
// core Veil process, every runtime name/binary contributed by a registered
// protocol plugin, and any non-plugin runtime binaries from the runtimeinstall
// catalog (e.g. WARP/sing-box).
func ManagedProcessPolicy() veilruntime.ManagedProcessPolicy {
	names := map[string]struct{}{"veil": {}}

	for _, p := range NewRegistry().All() {
		rp, ok := AsRuntimeProvider(p)
		if !ok {
			continue
		}
		rt := rp.RuntimeInstall("")
		if rt.Name != "" {
			names[rt.Name] = struct{}{}
		}
		if rt.Binary != "" {
			names[rt.Binary] = struct{}{}
		}
	}

	for _, rt := range runtimeinstall.Catalog("") {
		if rt.Name != "" {
			names[rt.Name] = struct{}{}
		}
		if rt.Binary != "" {
			names[rt.Binary] = struct{}{}
		}
	}

	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	return veilruntime.NewManagedProcessPolicyFor(out)
}
