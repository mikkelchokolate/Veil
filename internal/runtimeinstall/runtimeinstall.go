// Package runtimeinstall acquires and installs the external runtime binaries
// that Veil's managed systemd units invoke. After the plugin-based protocol
// refactor, protocol plugins contribute their own install descriptors via
// RuntimeProvider.RuntimeInstall; this package's Catalog only holds runtimes
// that are not tied to a protocol plugin, such as WARP (sing-box).
//
// Veil install only writes the Panel and the dormant managed unit files; before
// this package, those units pointed at /usr/local/bin/<binary> paths that were
// never created, so every protocol failed to start with systemd status
// 203/EXEC ("Failed to locate executable"). This package fills that gap by
// downloading each runtime from its upstream GitHub release (verifying SHA256
// checksums where the project publishes them) or, for runtimes that ship no
// release binaries, building them from source with `go install`.
package runtimeinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"debug/buildinfo"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Method describes how a runtime binary is acquired.
type Method string

const (
	// MethodRawBinary downloads a single executable file directly.
	MethodRawBinary Method = "raw-binary"
	// MethodArchive downloads a .tar.gz and extracts a single named binary.
	MethodArchive Method = "archive"
	// MethodGoInstall builds the binary from source with `go install`.
	MethodGoInstall Method = "go-install"
	// MethodCaddyNaive builds Caddy from source with the klzgrad/forwardproxy
	// (naive) fork, which vanilla Caddy releases do not include.
	MethodCaddyNaive Method = "caddy-naive"
)

// Runtime describes one protocol runtime binary Veil installs.
type Runtime struct {
	// Name is the protocol-facing label (e.g. "mieru", "hysteria2").
	Name string
	// Binary is the installed binary filename under the bin directory
	// (e.g. "mita", "hysteria", "sing-box", "caddy", "olcrtc").
	Binary string
	// Method selects the acquisition strategy.
	Method Method
	// Repo is the GitHub "owner/name" for release-based methods.
	Repo string
	// Version is an immutable upstream tag or commit. "latest" is never valid.
	Version string
	// SourceCommit pins source-built runtimes to an immutable 40-hex commit.
	SourceCommit string
	// SignaturePolicy identifies a pinned upstream signature policy when used.
	SignaturePolicy string
	// Integrity names the mandatory verification policy: upstream-checksum,
	// pinned-sha256, go-module-sum, or reproducible-go-build.
	Integrity string
	// PinnedSHA256 is required when Integrity is pinned-sha256.
	PinnedSHA256 string
	// VersionArgs invokes the staged binary's version probe. The reserved
	// __go_buildinfo__ probe verifies Go module build metadata for upstreams
	// that do not expose a version flag.
	VersionArgs []string
	// VersionCommand is the human/audit representation of the staged probe.
	VersionCommand string
	// VersionPattern must match the version probe output.
	VersionPattern string
	// AssetMatch selects the release asset for the current platform.
	AssetMatch func(assetName string) bool
	// ChecksumMatch selects the checksums asset, when the project ships one.
	ChecksumMatch func(assetName string) bool
	// SourcePackage is the Go package path for MethodGoInstall.
	SourcePackage string
	// Description is a human-readable sentence describing how the runtime is
	// acquired. It is used by the CLI to build protocol-agnostic help text.
	Description string
}

// Catalog returns the runtime install descriptors for non-plugin runtimes such
// as WARP (sing-box). Protocol plugins supply their own descriptors via
// RuntimeProvider.RuntimeInstall, so they are not duplicated here.
func Catalog(arch string) []Runtime {
	singBoxDigests := map[string]string{
		"amd64": "f48703461a15476951ac4967cdad339d986f4b8096b4eb3ff0829a500502d697",
		"arm64": "4742df6a4314e8ecc41736849fca6d73b8f9e91b6e8b06ee794ff17ba180579e",
	}
	const singBoxVersion = "v1.13.14"
	return []Runtime{
		{
			Name:           "warp",
			Binary:         "sing-box",
			Method:         MethodArchive,
			Repo:           "SagerNet/sing-box",
			Version:        singBoxVersion,
			Integrity:      "pinned-sha256",
			PinnedSHA256:   singBoxDigests[arch],
			VersionArgs:    []string{"version"},
			VersionCommand: "sing-box version",
			VersionPattern: `(?i)1\.13\.14`,
			Description:    "sing-box is downloaded from its pinned upstream GitHub release",
			AssetMatch: func(name string) bool {
				return name == "sing-box-1.13.14-linux-"+arch+".tar.gz"
			},
		},
	}
}

// Release is a subset of the GitHub Releases API response.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset is a single release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Options configures an installation run.
type Options struct {
	// BinDir is where binaries are written (default /usr/local/bin).
	BinDir string
	// CaddyCacheDir is where the Caddy build workspace is cached. Defaults to
	// a directory under the OS cache (e.g. /var/cache/veil or /tmp/veil-runtime).
	CaddyCacheDir string
	// Arch is the normalized architecture ("amd64" or "arm64").
	Arch string
	// HTTPClient performs release-metadata and download requests.
	HTTPClient *http.Client
	// FetchRelease resolves the latest release for a repo. Injectable for tests.
	FetchRelease func(ctx context.Context, repo string) (*Release, error)
	// FetchReleaseVersion resolves one exact immutable tag. When omitted, a
	// legacy injected FetchRelease is wrapped for tests; production uses the
	// GitHub releases/tags endpoint.
	FetchReleaseVersion func(ctx context.Context, repo, version string) (*Release, error)
	// Download fetches a URL's bytes. Injectable for tests.
	Download func(ctx context.Context, url string) ([]byte, error)
	// GoInstall builds a source package into BinDir. Injectable for tests.
	GoInstall func(ctx context.Context, binDir, sourcePackage string) error
	// BuildCaddy builds a Caddy binary with the naive forwardproxy fork.
	// Injectable for tests.
	BuildCaddy func(ctx context.Context, outPath string) error
	// EnsureGo provisions a Go toolchain if system go is unavailable, returning
	// the path to the go binary. If nil, methods requiring Go are skipped when
	// no system go is found.
	EnsureGo func(ctx context.Context) (string, error)
	// LookPath resolves an executable in PATH. Injectable for tests; defaults
	// to exec.LookPath. Used to detect whether a Go toolchain is present.
	LookPath func(string) (string, error)
	// RunVersion executes the staged binary's version command before publish.
	RunVersion func(ctx context.Context, binary string, args []string) (string, error)
	// VerifyPinnedSHA256 is injectable for deterministic tests; production
	// computes SHA-256 over the downloaded archive and compares the public digest.
	VerifyPinnedSHA256 func(body []byte, expected string) error
	// ReadGoBuildInfo is injectable for fake binaries in tests. Production reads
	// the Go build information embedded in the staged executable.
	ReadGoBuildInfo func(path string) (string, error)
	// AfterActivate is a test/fault-injection hook invoked after the atomic link
	// switch and before manifest finalization.
	AfterActivate func(runtime Runtime, activePath string) error
	// AfterPreserve is a fault-injection hook after an old regular binary has
	// moved to immutable storage but before the active symlink changes.
	AfterPreserve func(runtime Runtime, activePath string) error
	// Now supplies the clock.
	Now func() time.Time
}

// Result records the outcome for a single runtime.
type Result struct {
	Name            string
	Binary          string
	Path            string
	Method          Method
	Version         string
	VerifiedVersion string
	SHA256          string
	Installed       bool
	Skipped         bool
	SkipReason      string
	Err             error
}

const defaultBinDir = "/usr/local/bin"

// DefaultBinDir is the canonical install directory for runtime binaries.
func DefaultBinDir() string { return defaultBinDir }

func runtimeVersionProbeArgs(binary string, args []string) []string {
	return runtimeVersionProbeArgsAt(binary, "/probe/runtime", args)
}

func runtimeVersionProbeArgsAt(binary, sandboxBinary string, args []string) []string {
	return runtimeVersionProbeArgsAtRoot(binary, sandboxBinary, "", args)
}

func runtimeVersionProbeArgsAtRoot(binary, sandboxBinary, root string, args []string) []string {
	probe := []string{
		"--quiet", "--pipe", "--wait", "--collect",
		"--property=Type=exec", "--property=NoNewPrivileges=yes", "--property=DynamicUser=yes",
		"--property=PrivateNetwork=yes", "--property=PrivateDevices=yes", "--property=PrivateTmp=yes", "--property=PrivateMounts=yes",
		"--property=ProtectSystem=strict", "--property=ProtectHome=yes", "--property=ProtectProc=invisible", "--property=ProcSubset=pid",
		"--property=ProtectKernelTunables=yes", "--property=ProtectKernelModules=yes", "--property=ProtectControlGroups=yes",
		"--property=RestrictSUIDSGID=yes", "--property=LockPersonality=yes", "--property=MemoryDenyWriteExecute=yes",
		"--property=CapabilityBoundingSet=", "--property=RestrictAddressFamilies=AF_UNIX",
		"--property=SystemCallArchitectures=native",
		"--property=SystemCallFilter=@system-service ~@mount @privileged @resources @raw-io @reboot @swap @obsolete @debug",
		"--property=MemoryMax=128M", "--property=TasksMax=32", "--property=CPUQuota=25%", "--property=RuntimeMaxSec=15s",
		"--property=WorkingDirectory=/empty", "--property=UMask=0077",
		"--property=BindReadOnlyPaths=" + binary + ":" + sandboxBinary,
		"--setenv=PATH=/usr/bin:/bin", "--setenv=LANG=C", "--setenv=LC_ALL=C",
		"--", sandboxBinary,
	}
	if root != "" {
		probe = append(probe[:4], append([]string{"--property=RootDirectory=" + root, "--property=MountAPIVFS=yes"}, probe[4:]...)...)
	}
	return append(probe, args...)
}

func bubblewrapVersionProbeArgs(binary string, args []string) []string {
	const sandboxBinary = "/probe/runtime"
	probe := []string{
		"--die-with-parent", "--new-session", "--unshare-all",
		"--tmpfs", "/", "--dir", "/probe", "--dir", "/tmp", "--dir", "/proc", "--dir", "/dev",
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
		"--ro-bind", binary, sandboxBinary,
		"--clearenv", "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "LANG", "C", "--setenv", "LC_ALL", "C",
		"--", sandboxBinary,
	}
	return append(probe, args...)
}

func runtimeProbeDependencies(binary string) ([]string, error) {
	searchRoots := []string{
		"/lib/x86_64-linux-gnu", "/usr/lib/x86_64-linux-gnu", "/lib/aarch64-linux-gnu", "/usr/lib/aarch64-linux-gnu", "/lib64", "/usr/lib64", "/lib", "/usr/lib",
	}
	queue := []string{binary}
	seenFiles := map[string]struct{}{binary: {}}
	dependencies := make([]string, 0)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		file, err := elf.Open(current)
		if err != nil {
			// Scripts and static non-ELF probes have no loader dependencies.
			if current == binary {
				return nil, nil
			}
			return nil, err
		}
		libraries, err := file.ImportedLibraries()
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		for _, program := range file.Progs {
			if program.Type != elf.PT_INTERP {
				continue
			}
			body, readErr := io.ReadAll(program.Open())
			if readErr != nil {
				_ = file.Close()
				return nil, readErr
			}
			interpreter := strings.TrimRight(string(body), "\x00")
			if filepath.IsAbs(interpreter) {
				if _, ok := seenFiles[interpreter]; !ok {
					seenFiles[interpreter] = struct{}{}
					dependencies = append(dependencies, interpreter)
					queue = append(queue, interpreter)
				}
			}
		}
		_ = file.Close()
		for _, library := range libraries {
			resolved := ""
			for _, root := range searchRoots {
				candidate := filepath.Join(root, library)
				if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() {
					resolved = candidate
					break
				}
			}
			if resolved == "" {
				return nil, fmt.Errorf("resolve runtime probe library %s", library)
			}
			if _, ok := seenFiles[resolved]; !ok {
				seenFiles[resolved] = struct{}{}
				dependencies = append(dependencies, resolved)
				queue = append(queue, resolved)
			}
		}
	}
	sort.Strings(dependencies)
	return dependencies, nil
}

func prepareRuntimeProbeDependencyTargets(root string, dependencies []string) error {
	for _, dependency := range dependencies {
		target := filepath.Join(root, strings.TrimPrefix(dependency, "/"))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o400)
		if err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	}
	return nil
}

func insertSystemdProbeBinds(args []string, dependencies []string) []string {
	separator := slices.Index(args, "--")
	if separator < 0 {
		return args
	}
	binds := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		binds = append(binds, "--property=BindReadOnlyPaths="+dependency+":"+dependency)
	}
	result := append([]string(nil), args[:separator]...)
	result = append(result, binds...)
	return append(result, args[separator:]...)
}

func insertBubblewrapProbeBinds(args []string, dependencies []string) []string {
	separator := slices.Index(args, "--")
	if separator < 0 {
		return args
	}
	seenDirs := map[string]struct{}{`/`: {}}
	binds := make([]string, 0)
	for _, dependency := range dependencies {
		directory := filepath.Dir(dependency)
		parts := strings.Split(strings.TrimPrefix(directory, "/"), "/")
		current := ""
		for _, part := range parts {
			if part == "" {
				continue
			}
			current += "/" + part
			if _, ok := seenDirs[current]; !ok {
				seenDirs[current] = struct{}{}
				binds = append(binds, "--dir", current)
			}
		}
		binds = append(binds, "--ro-bind", dependency, dependency)
	}
	result := append([]string(nil), args[:separator]...)
	result = append(result, binds...)
	return append(result, args[separator:]...)
}

type boundedProbeOutput struct {
	mu        sync.Mutex
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (w *boundedProbeOutput) Write(body []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := w.limit - w.buffer.Len()
	if remaining > 0 {
		part := body
		if len(part) > remaining {
			part = part[:remaining]
		}
		_, _ = w.buffer.Write(part)
	}
	if len(body) > remaining {
		w.truncated = true
	}
	return len(body), nil
}

func (w *boundedProbeOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	result := w.buffer.String()
	if w.truncated {
		result += "…"
	}
	return result
}

func runSandboxedVersionProbe(ctx context.Context, binary string, args []string) (string, error) {
	dependencies, dependencyErr := runtimeProbeDependencies(binary)
	if dependencyErr != nil {
		return "", fmt.Errorf("resolve runtime probe dependencies: %w", dependencyErr)
	}
	command := "systemd-run"
	var commandArgs []string
	if _, err := os.Stat("/run/systemd/private"); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("inspect systemd manager: %w", err)
		}
		var lookupErr error
		command, lookupErr = exec.LookPath("bwrap")
		if lookupErr != nil {
			return "", errors.New("no supported runtime version-probe sandbox is available")
		}
		commandArgs = insertBubblewrapProbeBinds(bubblewrapVersionProbeArgs(binary, args), dependencies)
	} else {
		rootDir, createErr := os.MkdirTemp("/var/tmp", "veil-runtime-probe-root-*")
		if createErr != nil {
			return "", fmt.Errorf("create systemd probe root: %w", createErr)
		}
		defer os.RemoveAll(rootDir)
		if err := os.Chmod(rootDir, 0o755); err != nil {
			return "", err
		}
		for path, mode := range map[string]os.FileMode{"probe": 0o755, "empty": 0o555, "tmp": 0o1777} {
			if err := os.Mkdir(filepath.Join(rootDir, path), mode); err != nil {
				return "", fmt.Errorf("prepare systemd probe root: %w", err)
			}
		}
		if err := prepareRuntimeProbeDependencyTargets(rootDir, dependencies); err != nil {
			return "", fmt.Errorf("prepare systemd probe libraries: %w", err)
		}
		placeholderPath := filepath.Join(rootDir, "probe", "runtime")
		placeholder, createErr := os.OpenFile(placeholderPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o500)
		if createErr != nil {
			return "", fmt.Errorf("create systemd probe placeholder: %w", createErr)
		}
		if closeErr := placeholder.Close(); closeErr != nil {
			return "", fmt.Errorf("close systemd probe placeholder: %w", closeErr)
		}
		commandArgs = insertSystemdProbeBinds(runtimeVersionProbeArgsAtRoot(binary, "/probe/runtime", rootDir, args), dependencies)
	}
	probeOutput := &boundedProbeOutput{limit: 4096}
	cmd := exec.CommandContext(ctx, command, commandArgs...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Stdout = probeOutput
	cmd.Stderr = probeOutput
	err := cmd.Run()
	output := probeOutput.String()
	if err != nil {
		detail := strings.TrimSpace(output)
		if detail != "" {
			return output, fmt.Errorf("run %s sandboxed version probe: %w: %s", filepath.Base(binary), err, detail)
		}
		return output, fmt.Errorf("run %s sandboxed version probe: %w", filepath.Base(binary), err)
	}
	return output, nil
}

func (o Options) withDefaults() Options {
	legacyFetchRelease := o.FetchRelease
	if o.BinDir == "" {
		o.BinDir = defaultBinDir
	}
	if o.CaddyCacheDir == "" {
		o.CaddyCacheDir = filepath.Join(os.TempDir(), "veil-runtime")
	}
	if o.Arch == "" {
		o.Arch = "amd64"
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 120 * time.Second}
	}
	if o.FetchRelease == nil {
		o.FetchRelease = func(ctx context.Context, repo string) (*Release, error) {
			return fetchLatestRelease(ctx, o.HTTPClient, repo)
		}
	}
	if o.FetchReleaseVersion == nil {
		if legacyFetchRelease != nil {
			o.FetchReleaseVersion = func(ctx context.Context, repo, _ string) (*Release, error) {
				return legacyFetchRelease(ctx, repo)
			}
		} else {
			o.FetchReleaseVersion = func(ctx context.Context, repo, version string) (*Release, error) {
				return fetchReleaseByTag(ctx, o.HTTPClient, repo, version)
			}
		}
	}
	if o.Download == nil {
		o.Download = func(ctx context.Context, url string) ([]byte, error) {
			return downloadURL(ctx, o.HTTPClient, url)
		}
	}
	if o.GoInstall == nil {
		o.GoInstall = func(ctx context.Context, binDir, sourcePackage string) error {
			goBin, err := resolveGo(ctx, o.CaddyCacheDir, o.EnsureGo)
			if err != nil {
				return err
			}
			if goBin == "" {
				return fmt.Errorf("go toolchain not found")
			}
			return runGoInstall(ctx, goBin, binDir, sourcePackage)
		}
	}
	if o.BuildCaddy == nil {
		o.BuildCaddy = func(ctx context.Context, outPath string) error {
			goBin, err := resolveGo(ctx, o.CaddyCacheDir, o.EnsureGo)
			if err != nil {
				return err
			}
			if goBin == "" {
				return fmt.Errorf("go toolchain not found")
			}
			return runCaddyNaiveBuild(ctx, goBin, o.CaddyCacheDir, outPath)
		}
	}
	if o.EnsureGo == nil {
		tc := NewGoToolchain(o.CaddyCacheDir)
		o.EnsureGo = tc.Ensure
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.RunVersion == nil {
		o.RunVersion = runSandboxedVersionProbe
	}
	if o.VerifyPinnedSHA256 == nil {
		o.VerifyPinnedSHA256 = func(body []byte, expected string) error {
			digest := sha256.Sum256(body)
			if !strings.EqualFold(hex.EncodeToString(digest[:]), expected) {
				return errors.New("runtime archive checksum mismatch")
			}
			return nil
		}
	}
	if o.ReadGoBuildInfo == nil {
		o.ReadGoBuildInfo = func(path string) (string, error) {
			info, err := buildinfo.ReadFile(path)
			if err != nil {
				return "", err
			}
			var builder strings.Builder
			fmt.Fprintf(&builder, "%s@%s", info.Path, info.Main.Version)
			for _, dep := range info.Deps {
				fmt.Fprintf(&builder, "\n%s@%s", dep.Path, dep.Version)
			}
			return builder.String(), nil
		}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// InstallAll stages and verifies the complete requested runtime set before
// publishing one atomic generation.
func InstallAll(ctx context.Context, opts Options) []Result {
	opts = opts.withDefaults()
	return installRuntimeSet(ctx, opts, Catalog(opts.Arch))
}

func installRuntimes(ctx context.Context, opts Options, runtimes []Runtime) []Result {
	return installRuntimeSet(ctx, opts.withDefaults(), runtimes)
}

// Install stages, verifies, and atomically publishes a one-runtime set.
func Install(ctx context.Context, opts Options, runtime Runtime) Result {
	results := installRuntimeSet(ctx, opts.withDefaults(), []Runtime{runtime})
	if len(results) == 0 {
		return Result{Name: runtime.Name, Binary: runtime.Binary, Method: runtime.Method, Err: errors.New("runtime installation produced no result")}
	}
	return results[0]
}

// installOne acquires and installs one runtime into a transaction staging directory.
func installOne(ctx context.Context, opts Options, runtime Runtime) Result {
	opts = opts.withDefaults()
	result := Result{Name: runtime.Name, Binary: runtime.Binary, Method: runtime.Method}
	switch runtime.Method {
	case MethodRawBinary, MethodArchive, MethodGoInstall, MethodCaddyNaive:
	default:
		result.Err = fmt.Errorf("unsupported method %q", runtime.Method)
		return result
	}
	if err := validateRuntimeDescriptor(runtime); err != nil {
		result.Err = err
		return result
	}
	if err := os.MkdirAll(opts.BinDir, 0o755); err != nil {
		result.Err = fmt.Errorf("create bin dir: %w", err)
		return result
	}
	switch runtime.Method {
	case MethodRawBinary:
		path, version, verified, digest, err := installFromRelease(ctx, opts, runtime, false)
		result.Path, result.Version, result.VerifiedVersion, result.SHA256, result.Err = path, version, verified, digest, err
	case MethodArchive:
		path, version, verified, digest, err := installFromRelease(ctx, opts, runtime, true)
		result.Path, result.Version, result.VerifiedVersion, result.SHA256, result.Err = path, version, verified, digest, err
	case MethodGoInstall:
		goBin, err := resolveGo(ctx, opts.CaddyCacheDir, opts.EnsureGo)
		if err != nil {
			result.Err = err
			return result
		}
		if goBin == "" {
			result.Skipped = true
			result.SkipReason = "go toolchain not found; install Go to build olcrtc from source, then run: veil runtime install --only olcrtc"
			return result
		}
		path, verified, digest, err := installFromSource(ctx, opts, runtime)
		result.Path, result.Version, result.VerifiedVersion, result.SHA256, result.Err = path, runtime.Version, verified, digest, err
	case MethodCaddyNaive:
		goBin, err := resolveGo(ctx, opts.CaddyCacheDir, opts.EnsureGo)
		if err != nil {
			result.Err = err
			return result
		}
		if goBin == "" {
			result.Skipped = true
			result.SkipReason = "go toolchain not found; install Go to build Caddy with forward_proxy, then run: veil runtime install --only naiveproxy"
			return result
		}
		path, verified, digest, err := installCaddyNaive(ctx, opts, runtime)
		result.Path, result.Version, result.VerifiedVersion, result.SHA256, result.Err = path, runtime.Version, verified, digest, err
	default:
		result.Err = fmt.Errorf("unsupported method %q", runtime.Method)
	}
	result.Installed = result.Err == nil
	return result
}

func installFromRelease(ctx context.Context, opts Options, runtime Runtime, archive bool) (string, string, string, string, error) {
	release, err := opts.FetchReleaseVersion(ctx, runtime.Repo, runtime.Version)
	if err != nil {
		return "", "", "", "", fmt.Errorf("resolve %s release %s: %w", runtime.Repo, runtime.Version, err)
	}
	if release.TagName != runtime.Version {
		return "", "", "", "", fmt.Errorf("resolved release tag %q does not match pinned version %q", release.TagName, runtime.Version)
	}
	asset, ok := findAsset(release.Assets, runtime.AssetMatch)
	if !ok {
		return "", "", "", "", fmt.Errorf("release %s has no asset for linux/%s", release.TagName, opts.Arch)
	}
	body, err := opts.Download(ctx, asset.BrowserDownloadURL)
	if err != nil {
		return "", "", "", "", fmt.Errorf("download %s: %w", asset.Name, err)
	}
	switch runtime.Integrity {
	case "upstream-checksum":
		checksumAsset, ok := findAsset(release.Assets, runtime.ChecksumMatch)
		if !ok {
			return "", "", "", "", fmt.Errorf("release %s is missing the mandatory checksum asset for %s", release.TagName, asset.Name)
		}
		checksums, err := opts.Download(ctx, checksumAsset.BrowserDownloadURL)
		if err != nil {
			return "", "", "", "", fmt.Errorf("download checksums: %w", err)
		}
		if err := VerifyChecksum(body, asset.Name, runtime.Binary, checksums); err != nil {
			return "", "", "", "", err
		}
	case "pinned-sha256":
		if err := opts.VerifyPinnedSHA256(body, runtime.PinnedSHA256); err != nil {
			return "", "", "", "", fmt.Errorf("runtime archive checksum mismatch for %s: %w", asset.Name, err)
		}
	default:
		return "", "", "", "", fmt.Errorf("integrity policy %q is not valid for release runtime %s", runtime.Integrity, runtime.Name)
	}
	payload := body
	if archive {
		payload, err = ExtractArchiveBinary(body, runtime.Binary)
		if err != nil {
			return "", "", "", "", err
		}
	}
	path, verified, digest, err := publishVerifiedRuntime(ctx, opts, runtime, payload)
	return path, release.TagName, verified, digest, err
}

func installFromSource(ctx context.Context, opts Options, runtime Runtime) (string, string, string, error) {
	stageDir, err := os.MkdirTemp(opts.BinDir, ".veil-runtime-source-")
	if err != nil {
		return "", "", "", err
	}
	defer os.RemoveAll(stageDir)
	if err := opts.GoInstall(ctx, stageDir, runtime.SourcePackage); err != nil {
		return "", "", "", err
	}
	return publishVerifiedRuntimePath(ctx, opts, runtime, filepath.Join(stageDir, runtime.Binary))
}

func installCaddyNaive(ctx context.Context, opts Options, runtime Runtime) (string, string, string, error) {
	stageDir, err := os.MkdirTemp(opts.BinDir, ".veil-runtime-caddy-")
	if err != nil {
		return "", "", "", err
	}
	defer os.RemoveAll(stageDir)
	stagePath := filepath.Join(stageDir, runtime.Binary)
	if err := opts.BuildCaddy(ctx, stagePath); err != nil {
		return "", "", "", err
	}
	return publishVerifiedRuntimePath(ctx, opts, runtime, stagePath)
}

func publishVerifiedRuntime(ctx context.Context, opts Options, runtime Runtime, payload []byte) (string, string, string, error) {
	tmp, err := os.CreateTemp(opts.BinDir, ".veil-runtime-binary-")
	if err != nil {
		return "", "", "", err
	}
	stagePath := tmp.Name()
	defer os.Remove(stagePath)
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return "", "", "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", "", "", err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return "", "", "", err
	}
	if err := tmp.Close(); err != nil {
		return "", "", "", err
	}
	if err := syncRuntimeDirectory(opts.BinDir); err != nil {
		return "", "", "", err
	}
	return publishVerifiedRuntimePath(ctx, opts, runtime, stagePath)
}

func publishVerifiedRuntimePath(ctx context.Context, opts Options, runtime Runtime, stagePath string) (string, string, string, error) {
	info, err := os.Stat(stagePath)
	if err != nil {
		return "", "", "", err
	}
	if !info.Mode().IsRegular() {
		return "", "", "", fmt.Errorf("staged runtime %s is not a regular file", runtime.Name)
	}
	if err := os.Chmod(stagePath, 0o755); err != nil {
		return "", "", "", fmt.Errorf("set executable permissions on %s: %w", runtime.Binary, err)
	}
	verified, err := verifyRuntimeVersion(ctx, opts, runtime, stagePath)
	if err != nil {
		return "", "", "", err
	}
	digest, err := runtimeFileSHA256(stagePath)
	if err != nil {
		return "", "", "", err
	}
	if err := cleanupRuntimeStages(opts.BinDir); err != nil {
		return "", "", "", err
	}
	storeRoot := filepath.Join(opts.BinDir, ".veil-runtimes")
	versionDir := filepath.Join(storeRoot, runtime.Name, safeRuntimePathPart(runtime.Version), digest)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return "", "", "", err
	}
	target := filepath.Join(versionDir, runtime.Binary)
	if existingDigest, digestErr := runtimeFileSHA256(target); errors.Is(digestErr, os.ErrNotExist) {
		if err := os.Rename(stagePath, target); err != nil {
			return "", "", "", err
		}
		if err := syncRuntimeDirectory(versionDir); err != nil {
			return "", "", "", err
		}
	} else if digestErr != nil {
		return "", "", "", digestErr
	} else if existingDigest != digest {
		return "", "", "", errors.New("immutable runtime target digest mismatch")
	}
	active := filepath.Join(opts.BinDir, runtime.Binary)
	journal, err := beginRuntimeActivation(storeRoot, runtime, active, target, digest, opts.Now().UTC())
	if err != nil {
		return "", "", "", err
	}
	rollback := func(cause error) (string, string, string, error) {
		if rollbackErr := rollbackRuntimeActivation(storeRoot, journal); rollbackErr != nil {
			cause = errors.Join(cause, fmt.Errorf("restore previous runtime: %w", rollbackErr))
		}
		return "", "", "", cause
	}
	previousTarget, hadPrevious, err := preservePreviousRuntime(active, storeRoot, runtime)
	if err != nil {
		return rollback(err)
	}
	if previousTarget != journal.PreviousTarget || hadPrevious != journal.HadPrevious {
		return rollback(errors.New("runtime activation precondition changed after journal creation"))
	}
	if err := updateRuntimeActivationPhase(storeRoot, &journal, "previous-preserved", opts.Now().UTC()); err != nil {
		return rollback(err)
	}
	if opts.AfterPreserve != nil {
		if err := opts.AfterPreserve(runtime, active); err != nil {
			return rollback(err)
		}
	}
	if err := switchRuntimeSymlink(active, target); err != nil {
		return rollback(err)
	}
	if err := updateRuntimeActivationPhase(storeRoot, &journal, "active-switched", opts.Now().UTC()); err != nil {
		return rollback(err)
	}
	if opts.AfterActivate != nil {
		if err := opts.AfterActivate(runtime, active); err != nil {
			return rollback(err)
		}
	}
	if err := updateRuntimeManifest(storeRoot, runtime, target, verified, digest, opts.Now().UTC()); err != nil {
		return rollback(err)
	}
	if err := updateRuntimeActivationPhase(storeRoot, &journal, "manifest-committed", opts.Now().UTC()); err != nil {
		return rollback(err)
	}
	if err := removeRuntimeActivationJournal(storeRoot); err != nil {
		return "", "", "", err
	}
	return active, verified, digest, nil
}

type runtimeManifestEntry struct {
	Version         string    `json:"version"`
	VerifiedVersion string    `json:"verifiedVersion"`
	SHA256          string    `json:"sha256"`
	Path            string    `json:"path"`
	SourceCommit    string    `json:"sourceCommit,omitempty"`
	InstalledAt     time.Time `json:"installedAt"`
}

type runtimeManifest struct {
	Runtimes map[string]runtimeManifestEntry `json:"runtimes"`
}

func safeRuntimePathPart(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func cleanupRuntimeStages(binDir string) error {
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".veil-runtime-stage-") {
			if err := os.RemoveAll(filepath.Join(binDir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func preservePreviousRuntime(active, storeRoot string, runtime Runtime) (string, bool, error) {
	info, err := os.Lstat(active)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(active)
		return target, err == nil, err
	}
	if !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("active runtime %s is not regular or symlink", active)
	}
	digest, err := runtimeFileSHA256(active)
	if err != nil {
		return "", false, err
	}
	legacyDir := filepath.Join(storeRoot, runtime.Name, "legacy", digest)
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		return "", false, err
	}
	legacyTarget := filepath.Join(legacyDir, runtime.Binary)
	if _, err := os.Stat(legacyTarget); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(active, legacyTarget); err != nil {
			return "", false, err
		}
	} else if err != nil {
		return "", false, err
	} else if err := os.Remove(active); err != nil {
		return "", false, err
	}
	return legacyTarget, true, nil
}

func switchRuntimeSymlink(active, target string) error {
	temporary := active + ".activate-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.Symlink(target, temporary); err != nil {
		return err
	}
	defer os.Remove(temporary)
	if err := os.Rename(temporary, active); err != nil {
		return err
	}
	return syncRuntimeDirectory(filepath.Dir(active))
}

func updateRuntimeManifest(root string, runtime Runtime, target, verified, digest string, installedAt time.Time) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	manifestPath := filepath.Join(root, "manifest.json")
	manifest := runtimeManifest{Runtimes: make(map[string]runtimeManifestEntry)}
	if body, err := os.ReadFile(manifestPath); err == nil {
		if len(body) > 1<<20 {
			return errors.New("runtime manifest exceeds size limit")
		}
		if err := json.Unmarshal(body, &manifest); err != nil {
			return fmt.Errorf("decode runtime manifest: %w", err)
		}
		if manifest.Runtimes == nil {
			manifest.Runtimes = make(map[string]runtimeManifestEntry)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	manifest.Runtimes[runtime.Name] = runtimeManifestEntry{
		Version: runtime.Version, VerifiedVersion: verified, SHA256: digest,
		Path: target, SourceCommit: runtime.SourceCommit, InstalledAt: installedAt,
	}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(root, ".manifest-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(append(body, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, manifestPath); err != nil {
		return err
	}
	return syncRuntimeDirectory(root)
}

var semanticVersionPattern = regexp.MustCompile(`(?i)v?([0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?)`)

func verifyRuntimeVersion(ctx context.Context, opts Options, runtime Runtime, stagePath string) (string, error) {
	var output string
	if len(runtime.VersionArgs) == 1 && runtime.VersionArgs[0] == "__go_buildinfo__" {
		var err error
		output, err = opts.ReadGoBuildInfo(stagePath)
		if err != nil {
			return "", fmt.Errorf("read %s Go build metadata: %w", runtime.Name, err)
		}
		commit := strings.TrimPrefix(runtime.Version, "v")
		if len(commit) > 12 {
			commit = commit[:12]
		}
		if commit == "" || !strings.Contains(output, commit) {
			return "", fmt.Errorf("%s build metadata does not contain pinned commit %s", runtime.Name, commit)
		}
	} else {
		var err error
		output, err = opts.RunVersion(ctx, stagePath, runtime.VersionArgs)
		if err != nil {
			return "", err
		}
	}
	matcher, err := regexp.Compile(runtime.VersionPattern)
	if err != nil {
		return "", fmt.Errorf("invalid %s version pattern: %w", runtime.Name, err)
	}
	if !matcher.MatchString(output) {
		return "", fmt.Errorf("%s version output %q does not satisfy %q", runtime.Name, strings.TrimSpace(output), runtime.VersionPattern)
	}
	expected := semanticVersionPattern.FindStringSubmatch(runtime.Version)
	actual := semanticVersionPattern.FindStringSubmatch(output)
	if len(expected) > 1 {
		if len(actual) <= 1 || expected[1] != actual[1] {
			return "", fmt.Errorf("%s reports version %q; expected %q", runtime.Name, strings.TrimSpace(output), runtime.Version)
		}
	}
	return strings.TrimSpace(output), nil
}

func runtimeFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func syncRuntimeDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func validateRuntimeDescriptor(runtime Runtime) error {
	if strings.TrimSpace(runtime.Name) == "" || strings.TrimSpace(runtime.Binary) == "" {
		return errors.New("runtime name and binary are required")
	}
	version := strings.TrimSpace(runtime.Version)
	if version == "" || strings.EqualFold(version, "latest") || strings.Contains(strings.ToLower(runtime.SourcePackage), "@latest") {
		return fmt.Errorf("runtime %s must pin an immutable version", runtime.Name)
	}
	if len(runtime.VersionArgs) == 0 || strings.TrimSpace(runtime.VersionCommand) == "" || strings.TrimSpace(runtime.VersionPattern) == "" {
		return fmt.Errorf("runtime %s must declare a version probe", runtime.Name)
	}
	if _, err := regexp.Compile(runtime.VersionPattern); err != nil {
		return fmt.Errorf("runtime %s version pattern: %w", runtime.Name, err)
	}
	switch runtime.Integrity {
	case "upstream-checksum":
		if runtime.ChecksumMatch == nil {
			return fmt.Errorf("runtime %s requires a checksum asset selector", runtime.Name)
		}
	case "pinned-sha256":
		digest, err := hex.DecodeString(runtime.PinnedSHA256)
		if err != nil || len(digest) != sha256.Size {
			return fmt.Errorf("runtime %s has an invalid pinned SHA-256", runtime.Name)
		}
	case "go-module-sum":
		if runtime.Method != MethodGoInstall || !strings.HasSuffix(runtime.SourcePackage, "@"+runtime.Version) {
			return fmt.Errorf("runtime %s source package is not pinned to %s", runtime.Name, runtime.Version)
		}
	case "reproducible-go-build":
		if runtime.Method != MethodCaddyNaive {
			return fmt.Errorf("runtime %s has incompatible integrity policy %s", runtime.Name, runtime.Integrity)
		}
	default:
		return fmt.Errorf("runtime %s has no mandatory integrity policy", runtime.Name)
	}
	if (runtime.Method == MethodRawBinary || runtime.Method == MethodArchive) && (runtime.Repo == "" || runtime.AssetMatch == nil) {
		return fmt.Errorf("release runtime %s has an incomplete source descriptor", runtime.Name)
	}
	return nil
}

// runCaddyNaiveBuild builds a Caddy binary with the klzgrad/forwardproxy
// (naive) fork. It creates a self-contained Go module in cacheDir/build-caddy,
// pins Caddy v2.11.4 and the naive forwardproxy fork via a replace directive,
// and compiles a static binary to outPath.
func runCaddyNaiveBuild(ctx context.Context, goBin, cacheDir, outPath string) error {
	if resolved, err := exec.LookPath(goBin); err == nil {
		goBin = resolved
	}
	buildDir := filepath.Join(cacheDir, "build-caddy")
	// Ensure a clean build dir so the build is idempotent: a leftover
	// go.mod from a previous run makes `go mod init` fail, breaking
	// re-runs of `veil runtime install`.
	if err := os.RemoveAll(buildDir); err != nil {
		return fmt.Errorf("clean caddy build dir: %w", err)
	}
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return fmt.Errorf("create caddy build dir: %w", err)
	}

	// Write main.go — imports caddy and the naive forwardproxy module.
	mainGo := filepath.Join(buildDir, "main.go")
	if err := os.WriteFile(mainGo, []byte(`package main

import (
	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	_ "github.com/caddyserver/forwardproxy"
)

func main() {
	caddycmd.Main()
}
`), 0o644); err != nil {
		return fmt.Errorf("write main.go: %w", err)
	}

	// Initialize module, pin caddy and the naive forwardproxy fork.
	cmds := [][]string{
		{goBin, "mod", "init", "caddy"},
		{goBin, "mod", "edit", "-require", "github.com/caddyserver/caddy/v2@v2.11.4"},
		{goBin, "mod", "edit", "-replace", "github.com/caddyserver/forwardproxy=github.com/klzgrad/forwardproxy@d62c80d3dd2c706b6b87579844d2397bddd18317"},
		{goBin, "mod", "tidy"},
		// go mod verify ensures every downloaded module matches go.sum before build.
		{goBin, "mod", "verify"},
	}
	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = buildDir
		cmd.Env = goBuildEnv(goBin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
		}
	}

	// Build the binary.
	buildArgs := []string{goBin, "build", "-mod=readonly", "-o", outPath, "-ldflags=-s -w", "-trimpath", "."}
	cmd := exec.CommandContext(ctx, buildArgs[0], buildArgs[1:]...)
	cmd.Dir = buildDir
	cmd.Env = append(goBuildEnv(goBin), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("caddy build: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// atomicFile is the subset of *os.File used by writeBinaryAtomic. It is
// implemented by *os.File in production and by test doubles in tests.
type atomicFile interface {
	io.Writer
	Name() string
	Chmod(mode os.FileMode) error
	Close() error
}

// writeBinaryAtomicCreateTemp is swapped in tests to exercise error paths.
var writeBinaryAtomicCreateTemp = func(dir, pattern string) (atomicFile, error) {
	return os.CreateTemp(dir, pattern)
}

func writeBinaryAtomic(path string, body []byte) error {
	dir := filepath.Dir(path)
	tmp, err := writeBinaryAtomicCreateTemp(dir, ".veil-runtime-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// findAsset returns the first asset accepted by match.
func findAsset(assets []Asset, match func(string) bool) (Asset, bool) {
	if match == nil {
		return Asset{}, false
	}
	candidates := make([]Asset, 0, len(assets))
	for _, asset := range assets {
		if match(asset.Name) {
			candidates = append(candidates, asset)
		}
	}
	if len(candidates) == 0 {
		return Asset{}, false
	}
	// Deterministic selection: shortest name wins, then lexical. This favors the
	// plain asset (e.g. "sing-box-...-linux-amd64.tar.gz") over decorated
	// variants when the matcher is intentionally permissive.
	sort.Slice(candidates, func(i, j int) bool {
		if len(candidates[i].Name) != len(candidates[j].Name) {
			return len(candidates[i].Name) < len(candidates[j].Name)
		}
		return candidates[i].Name < candidates[j].Name
	})
	return candidates[0], true
}

// ExtractArchiveBinary returns the named binary's bytes from a .tar.gz archive,
// matching on the base filename so archives that nest the binary in a versioned
// directory (sing-box) and those that don't (mieru) both work.
func ExtractArchiveBinary(body []byte, binary string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) == binary {
			const maxBinary = 200 * 1024 * 1024
			return io.ReadAll(io.LimitReader(tr, maxBinary))
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", binary)
}

// VerifyChecksum confirms body's checksum matches the value recorded for the
// asset. Upstream checksum files reference either the asset filename (mieru,
// caddy) or a build path ending in the binary name (hysteria's hashes.txt), so
// both forms are accepted. The digest algorithm is selected by the recorded
// hex length: 64 chars -> SHA-256 (mieru, hysteria), 128 chars -> SHA-512
// (caddy).
func VerifyChecksum(body []byte, assetName, binary string, checksums []byte) error {
	want := extractChecksum(string(checksums), assetName, binary)
	if want == "" {
		return fmt.Errorf("no checksum recorded for %s", assetName)
	}
	var got string
	switch len(want) {
	case 64:
		sum := sha256.Sum256(body)
		got = hex.EncodeToString(sum[:])
	case 128:
		sum := sha512.Sum512(body)
		got = hex.EncodeToString(sum[:])
	default:
		return fmt.Errorf("unsupported checksum length %d for %s", len(want), assetName)
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", assetName, want, got)
	}
	return nil
}

func extractChecksum(checksums, assetName, binary string) string {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		hash := fields[0]
		file := fields[len(fields)-1]
		base := file
		if idx := strings.LastIndexAny(file, "/\\"); idx != -1 {
			base = file[idx+1:]
		}
		if file == assetName || base == assetName || (binary != "" && base == binary) {
			return hash
		}
	}
	return ""
}

func fetchReleaseByTag(ctx context.Context, client *http.Client, repo, version string) (*Release, error) {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, url.PathEscape(version))
	return fetchReleaseAt(ctx, client, endpoint, "")
}

func fetchLatestRelease(ctx context.Context, client *http.Client, repo string) (*Release, error) {
	return fetchReleaseAt(ctx, client, fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo), repo)
}

func fetchReleaseAt(ctx context.Context, client *http.Client, endpoint, latestFallbackRepo string) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "veil")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		closeErr := resp.Body.Close()
		if closeErr != nil {
			return nil, closeErr
		}
		if latestFallbackRepo != "" && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) {
			return fetchLatestReleaseWeb(ctx, client, latestFallbackRepo)
		}
		return nil, fmt.Errorf("GitHub API %s: %s", endpoint, resp.Status)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return parseRelease(body)
}

// fetchLatestReleaseWeb is a fallback for GitHub API rate limits. It follows
// the /releases/latest redirect to discover the tag, then scrapes the
// expanded_assets page for asset names and download URLs.
func fetchLatestReleaseWeb(ctx context.Context, client *http.Client, repo string) (*Release, error) {
	tag, err := resolveLatestTagWeb(ctx, client, repo)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://github.com/%s/releases/expanded_assets/%s", repo, url.PathEscape(tag))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "veil")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub web %s expanded assets: %s", repo, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	assets, err := parseExpandedAssets(string(body), tag, repo)
	if err != nil {
		return nil, err
	}
	return &Release{TagName: tag, Assets: assets}, nil
}

// resolveLatestTagWeb follows the GitHub /releases/latest redirect and returns
// the URL-encoded tag name (e.g. "app%2Fv2.9.2" or "v3.34.0").
func resolveLatestTagWeb(ctx context.Context, client *http.Client, repo string) (string, error) {
	latestURL := fmt.Sprintf("https://github.com/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, latestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "veil")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" && resp.Request != nil && resp.Request.URL != nil {
		loc = resp.Request.URL.String()
	}
	if loc == "" {
		return "", fmt.Errorf("GitHub web %s latest: no redirect location", repo)
	}
	locURL, err := url.Parse(loc)
	if err != nil {
		return "", fmt.Errorf("GitHub web %s latest: parse location %q: %w", repo, loc, err)
	}
	parts := strings.Split(strings.Trim(locURL.Path, "/"), "/")
	// Expected path: owner/repo/releases/tag/<tag>
	if len(parts) < 5 || parts[2] != "releases" || parts[3] != "tag" {
		return "", fmt.Errorf("GitHub web %s latest: unexpected location path %q", repo, locURL.Path)
	}
	// The tag is everything after /tag/; join it back in case the tag contains slashes.
	return path.Join(parts[4:]...), nil
}

// parseExpandedAssets extracts asset names and download URLs from the GitHub
// expanded_assets HTML page. Asset paths are relative ("/owner/repo/releases/...").
func parseExpandedAssets(html, tag, repo string) ([]Asset, error) {
	// Match hrefs that point at a release download for this tag.
	// Example: href="/apernet/hysteria/releases/download/app%2Fv2.9.2/hysteria-linux-amd64"
	pattern := fmt.Sprintf(`href="/(%s/releases/download/%s/([^"]+))"`, regexp.QuoteMeta(repo), regexp.QuoteMeta(url.PathEscape(tag)))
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var assets []Asset
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		name := m[2]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		assets = append(assets, Asset{
			Name:               name,
			BrowserDownloadURL: "https://github.com/" + m[1],
		})
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("GitHub web %s release %s: no assets found", repo, tag)
	}
	return assets, nil
}

func downloadURL(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "veil")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	const maxDownload = 200 * 1024 * 1024
	return io.ReadAll(io.LimitReader(resp.Body, maxDownload))
}

func runGoInstall(ctx context.Context, goBin, binDir, sourcePackage string) error {
	cmd := exec.CommandContext(ctx, goBin, "install", sourcePackage)
	cmd.Env = append(goBuildEnv(goBin), "GOBIN="+binDir, "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			return fmt.Errorf("go install %s: %s: %w", sourcePackage, trimmed, err)
		}
		return fmt.Errorf("go install %s: %w", sourcePackage, err)
	}
	return nil
}
