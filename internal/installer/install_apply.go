package installer

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/managedfiles"
)

var (
	effectiveUID        = os.Geteuid
	lookupGroup         = user.LookupGroup
	chownPath           = os.Chown
	chmodPath           = os.Chmod
	applyQUICUDPBuffers = hostenv.ApplyQUICUDPBuffers
	quicBufferWarn      = func(format string, args ...any) { fmt.Fprintf(os.Stderr, format, args...) }
)

type ApplyPaths struct {
	EtcDir      string
	VarDir      string
	SystemdDir  string
	BackupDir   string
	VeilBinary  string
	CaddyBinary string
}

type ApplyResult struct {
	CaddyfilePath     string
	Hysteria2Path     string
	FallbackIndexPath string
	WrittenFiles      []string
	BackupID          string
}

// newUFWApplier is overridable in tests so that firewall success and error
// paths can be exercised without depending on a real ufw installation.
var newUFWApplier = firewall.NewUFWApplier

type InstallApply struct {
	profile         RURecommendedProfile
	paths           ApplyPaths
	firewallActions []firewall.Rule
}

func NewInstallApply(profile RURecommendedProfile, paths ApplyPaths) InstallApply {
	return InstallApply{profile: profile, paths: paths}
}

// NewInstallApplyWithPlan creates an installer that also applies firewall rules.
func NewInstallApplyWithPlan(profile RURecommendedProfile, paths ApplyPaths, plan InstallPlan) InstallApply {
	return InstallApply{profile: profile, paths: paths, firewallActions: plan.FirewallActions}
}

func (a InstallApply) Apply() (ApplyResult, error) {
	files, err := desiredManagedFiles(a.profile, a.paths)
	if err != nil {
		return ApplyResult{}, err
	}
	result := ApplyResult{
		CaddyfilePath:     filepath.Join(a.paths.EtcDir, "generated", "caddy", "config.json"),
		Hysteria2Path:     filepath.Join(a.paths.EtcDir, "generated", "hysteria2", "server.yaml"),
		FallbackIndexPath: filepath.Join(a.paths.EtcDir, "www", "index.html"),
	}
	if a.paths.BackupDir != "" {
		existingPaths := make([]string, 0, len(files))
		for _, file := range files {
			existingPaths = append(existingPaths, file.Path)
		}
		backupID, err := backup.NewLifecycle(a.paths.BackupDir).BackupExisting(existingPaths)
		if err != nil {
			return ApplyResult{}, err
		}
		result.BackupID = backupID
	}
	for _, file := range files {
		if err := writeManagedFile(file.Path, file.Content, file.Mode); err != nil {
			return ApplyResult{}, err
		}
		result.WrittenFiles = append(result.WrittenFiles, file.Path)
	}
	if err := chownSecretsForVeilGroup(result.WrittenFiles); err != nil {
		return result, err
	}
	if err := backup.EnsurePassphraseFile(filepath.Join(a.paths.EtcDir, "backup.passphrase")); err != nil {
		return result, fmt.Errorf("write backup passphrase: %w", err)
	}

	if len(a.firewallActions) > 0 {
		applier := newUFWApplier()
		if err := applier.ApplySafely(a.firewallActions); err != nil {
			return result, fmt.Errorf("apply firewall rules: %w", err)
		}
	}
	if err := applyQUICUDPBuffers(); err != nil {
		// Live sysctl can be rejected on OpenVZ/LXC after secrets and firewall
		// rules are already on disk. Persist-or-warn; do not fail the install.
		quicBufferWarn("WARNING: could not tune QUIC UDP buffers: %v\n", err)
	}

	return result, nil
}

func ApplyRURecommendedProfile(profile RURecommendedProfile, paths ApplyPaths) (ApplyResult, error) {
	return NewInstallApply(profile, paths).Apply()
}

// ApplyRURecommendedProfileWithPlan applies the install profile and executes the firewall rules from the install plan.
func ApplyRURecommendedProfileWithPlan(profile RURecommendedProfile, paths ApplyPaths, plan InstallPlan) (ApplyResult, error) {
	return NewInstallApplyWithPlan(profile, paths, plan).Apply()
}

// chownSecretsForVeilGroup restores the post-#601 ownership contract on files
// written by install/repair: root ownership everywhere, group veil for
// panel-only secrets, group veil-proxy for runtime-shared material. It also
// fixes every ancestor directory from the file up to and including the
// outermost shared subtree (generated/, tls/, panel/, certs/, www/) so a tree
// created without a prior Migrate stays traversable by veil-proxy
// (audit #531).
func chownSecretsForVeilGroup(paths []string) error {
	if effectiveUID() != 0 {
		// A non-root process cannot chown at all; on the packaged layout this
		// would silently leave secrets unreadable, so fail closed there
		// (audit #532). Scratch/test trees keep the historical skip because
		// no production ownership contract applies to them.
		for _, path := range paths {
			if needsVeilGroupRead(path) && underProductionVeilRoot(path) {
				return fmt.Errorf("cannot establish managed file ownership as non-root user (euid=%d): %s", effectiveUID(), path)
			}
		}
		return nil
	}
	veilGID, err := resolveGroupGID("veil")
	if err != nil {
		return err
	}
	// Runtime-shared material must land in the veil-proxy group; a missing or
	// unparseable veil-proxy account must not silently fall back to the veil
	// group, which the protocol units cannot read (audit #532).
	proxyGID, err := resolveGroupGID("veil-proxy")
	if err != nil {
		return err
	}
	seenDirs := map[string]struct{}{}
	for _, path := range paths {
		if !needsVeilGroupRead(path) {
			continue
		}
		ownerGid := veilGID
		if isRuntimeSharedConfig(path) {
			ownerGid = proxyGID
		}
		if err := chownPath(path, 0, ownerGid); err != nil {
			return fmt.Errorf("chown %s for veil group: %w", path, err)
		}
		if err := chmodPath(path, 0o640); err != nil {
			return fmt.Errorf("chmod %s for veil group: %w", path, err)
		}
		if !isRuntimeSharedConfig(path) {
			continue
		}
		for _, dir := range runtimeSharedParentDirs(path) {
			if _, ok := seenDirs[dir]; ok {
				continue
			}
			seenDirs[dir] = struct{}{}
			if err := chownPath(dir, 0, ownerGid); err != nil {
				return fmt.Errorf("chown %s for veil group: %w", dir, err)
			}
			if err := chmodPath(dir, 0o750); err != nil {
				return fmt.Errorf("chmod %s for veil group: %w", dir, err)
			}
		}
	}
	return nil
}

// resolveGroupGID resolves a group name to its gid. Group lookup is used
// rather than the user's primary gid: an account created with a different
// primary group would otherwise silently pick the wrong owner group
// (audit #532).
func resolveGroupGID(name string) (int, error) {
	g, err := lookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf("resolve %s group: %w", name, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("parse %s gid %q: %w", name, g.Gid, err)
	}
	return gid, nil
}

// underProductionVeilRoot reports whether path lives inside the packaged
// /etc/veil or /var/lib/veil trees, where the ownership contract applies.
func underProductionVeilRoot(path string) bool {
	slash := filepath.ToSlash(path)
	return slash == "/etc/veil" || strings.HasPrefix(slash, "/etc/veil/") ||
		slash == "/var/lib/veil" || strings.HasPrefix(slash, "/var/lib/veil/")
}

// runtimeSharedParentDirs returns the ancestor directories of path from the
// file's directory up to and including the outermost runtime-shared subtree
// component (generated, tls, panel, certs, www). The subtree root's parent
// (e.g. /etc/veil, which stays root:veil) is deliberately excluded.
func runtimeSharedParentDirs(path string) []string {
	var dirs []string
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		parent := filepath.Dir(dir)
		if sharedSubtreeName(filepath.Base(dir)) && !sharedSubtreeName(filepath.Base(parent)) {
			break
		}
		if parent == dir {
			break
		}
	}
	return dirs
}

// sharedSubtreeName reports whether base names a managed runtime-shared
// subtree directly under the Veil etc directory.
func sharedSubtreeName(base string) bool {
	switch base {
	case "generated", "tls", "panel", "certs", "www":
		return true
	}
	return false
}

func needsVeilGroupRead(path string) bool {
	base := filepath.Base(path)
	if base == "veil.env" || strings.HasSuffix(base, ".key") || strings.HasSuffix(base, ".crt") {
		return true
	}
	// Everything under a runtime-shared subtree must stay group-readable:
	// that is what makes the subtree shared in the first place.
	return isRuntimeSharedConfig(path)
}

func isGeneratedConfig(path string) bool {
	return strings.Contains(filepath.ToSlash(path), "/generated/")
}

// isRuntimeSharedConfig reports paths the protocol units (User=veil-proxy)
// read directly: generated configs and the shared panel TLS material. The
// panel account is a supplementary veil-proxy member, so a single group
// covers both readers. Panel-only secrets such as state.key and veil.env
// stay in the veil group.
func isRuntimeSharedConfig(path string) bool {
	slash := filepath.ToSlash(path)
	return isGeneratedConfig(path) ||
		strings.Contains(slash, "/panel/") ||
		strings.Contains(slash, "/tls/") ||
		strings.Contains(slash, "/certs/") ||
		strings.Contains(slash, "/www/")
}

func writeManagedFile(path string, content string, mode os.FileMode) error {
	return managedfiles.WriteFile(path, content, mode)
}
