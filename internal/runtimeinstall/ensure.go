package runtimeinstall

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type cachedRuntimeVerification struct {
	identity, verifiedVersion string
}

type runtimeVerificationCache struct {
	mu     sync.Mutex
	byName map[string]cachedRuntimeVerification
}

// NewEnsurer caches successful probes for one helper lifetime. Each call still
// proves installer provenance and hashes the current executable before reuse.
func NewEnsurer() func(context.Context, Options, Runtime) Result {
	cache := &runtimeVerificationCache{byName: make(map[string]cachedRuntimeVerification)}
	return func(ctx context.Context, opts Options, runtime Runtime) Result {
		return ensure(ctx, opts, runtime, cache)
	}
}

func (c *runtimeVerificationCache) lookup(name, identity string) string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.byName[name]
	if entry.identity == identity {
		return entry.verifiedVersion
	}
	return ""
}

func (c *runtimeVerificationCache) remember(name, identity, verified string) {
	if c == nil || verified == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byName[name] = cachedRuntimeVerification{identity, verified}
}

func runtimeVerificationIdentity(opts Options, runtime Runtime, path, digest string) string {
	fields := []string{path, opts.Arch, digest, runtime.Name, runtime.Binary, string(runtime.Method),
		runtime.Repo, runtime.Version, runtime.SourceCommit, runtime.SourcePackage,
		runtime.Integrity, runtime.PinnedSHA256, runtime.SignaturePolicy, runtime.VersionCommand, runtime.VersionPattern}
	return strings.Join(append(fields, runtime.VersionArgs...), "\x00")
}

// Ensure reuses only a regular 0755 executable with matching installer provenance
// and the pinned version probe. Missing, modified or older binaries are repaired.
func Ensure(ctx context.Context, opts Options, runtime Runtime) Result {
	return ensure(ctx, opts, runtime, nil)
}

func ensure(ctx context.Context, opts Options, runtime Runtime, cache *runtimeVerificationCache) Result {
	opts = opts.withDefaults()
	if err := ctx.Err(); err != nil {
		return Result{Name: runtime.Name, Binary: runtime.Binary, Method: runtime.Method, Err: err}
	}
	if err := validateRuntimeDescriptor(runtime); err != nil {
		return Result{Name: runtime.Name, Binary: runtime.Binary, Method: runtime.Method, Err: err}
	}
	opts.progress("checking", runtime.Name)
	active := filepath.Join(opts.BinDir, runtime.Binary)
	manifest, ok := readRuntimeGenerationManifest(filepath.Join(opts.BinDir, runtimeSetManifestName))
	info, err := os.Lstat(active)
	if ok && err == nil && info.Mode() == 0o755 {
		digest, err := runtimeFileSHA256(active)
		if err == nil && digest != "" && digest == manifest.Digests[runtime.Binary] {
			identity := runtimeVerificationIdentity(opts, runtime, active, digest)
			verified := cache.lookup(runtime.Name, identity)
			if verified == "" {
				verified, err = verifyRuntimeVersion(ctx, opts, runtime, active)
				if err == nil {
					cache.remember(runtime.Name, identity, verified)
				}
			}
			if err == nil {
				opts.progress("reused", runtime.Name)
				return Result{Name: runtime.Name, Binary: runtime.Binary, Path: active,
					Method: runtime.Method, Version: runtime.Version, VerifiedVersion: verified,
					SHA256: digest, Installed: true}
			}
		}
	}
	result := Install(ctx, opts, runtime)
	if cache != nil && result.Err == nil && result.Installed {
		if current, ok := readRuntimeGenerationManifest(filepath.Join(opts.BinDir, runtimeSetManifestName)); ok && current.Digests[runtime.Binary] == result.SHA256 {
			cache.remember(runtime.Name, runtimeVerificationIdentity(opts, runtime, result.Path, result.SHA256), result.VerifiedVersion)
		}
	}
	return result
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
