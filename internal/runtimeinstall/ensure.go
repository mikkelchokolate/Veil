package runtimeinstall

import (
	"context"
	"os"
	"path/filepath"
)

// Ensure reuses only a regular executable with matching installer provenance
// and the pinned version probe. Missing, modified or older binaries are repaired.
func Ensure(ctx context.Context, opts Options, runtime Runtime) Result {
	opts = opts.withDefaults()
	if err := validateRuntimeDescriptor(runtime); err != nil {
		return Result{Name: runtime.Name, Binary: runtime.Binary, Method: runtime.Method, Err: err}
	}
	opts.progress("checking", runtime.Name)
	active := filepath.Join(opts.BinDir, runtime.Binary)
	manifest, ok := readRuntimeGenerationManifest(filepath.Join(opts.BinDir, runtimeSetManifestName))
	info, err := os.Lstat(active)
	if ok && err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
		digest, err := runtimeFileSHA256(active)
		if err == nil && digest != "" && digest == manifest.Digests[runtime.Binary] {
			if verified, err := verifyRuntimeVersion(ctx, opts, runtime, active); err == nil {
				opts.progress("reused", runtime.Name)
				return Result{Name: runtime.Name, Binary: runtime.Binary, Path: active,
					Method: runtime.Method, Version: runtime.Version, VerifiedVersion: verified,
					SHA256: digest, Installed: true}
			}
		}
	}
	return Install(ctx, opts, runtime)
}

func (o Options) progress(stage, name string) {
	if o.Progress != nil {
		o.Progress(stage, name)
	}
}

type buildHomeContextKey struct{}

// ContextWithBuildHome lets the privileged helper use a private root-owned
// build directory when its systemd sandbox makes the account home inaccessible.
// The directory is supplied by trusted host configuration, never an IPC caller.
func ContextWithBuildHome(ctx context.Context, home string) context.Context {
	return context.WithValue(ctx, buildHomeContextKey{}, home)
}
