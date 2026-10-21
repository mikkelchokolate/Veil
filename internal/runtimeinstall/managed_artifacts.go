package runtimeinstall

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxRuntimeGenerationManifestBytes bounds the provenance record read by
// ManagedRuntimeArtifacts; the installer writes a few hundred bytes.
const maxRuntimeGenerationManifestBytes = 1 << 20

// ManagedRuntimeArtifacts returns the files under binDir that Veil's runtime
// installer provably wrote: every runtime binary recorded in the generation
// manifest (.veil-runtime-generation.json) whose current bytes still match
// the recorded SHA-256, followed by the installer's own bookkeeping files
// (the manifest and the activation lock). `veil uninstall` removes exactly
// this set (issue #1289).
//
// Provenance is content-based on purpose: a binary the operator replaced
// after Veil installed it (a distro or hand-built caddy at the same path), a
// symlink, a directory or any other non-regular node under a recorded name
// is not Veil's any more and is never returned. A missing, oversized,
// non-regular or undecodable manifest yields nil — without proof nothing is
// removed.
func ManagedRuntimeArtifacts(binDir string) []string {
	if strings.TrimSpace(binDir) == "" {
		return nil
	}
	manifestPath := filepath.Join(binDir, runtimeSetManifestName)
	manifest, ok := readRuntimeGenerationManifest(manifestPath)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(manifest.Digests))
	for name := range manifest.Digests {
		names = append(names, name)
	}
	sort.Strings(names)
	paths := make([]string, 0, len(names)+2)
	for _, name := range names {
		if !safeManagedRuntimeName(name) {
			continue
		}
		path := filepath.Join(binDir, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		digest, err := runtimeFileSHA256(path)
		if err != nil || digest != strings.ToLower(strings.TrimSpace(manifest.Digests[name])) {
			continue
		}
		paths = append(paths, path)
	}
	paths = append(paths, manifestPath)
	lockPath := filepath.Join(binDir, runtimeSetLockName)
	if info, err := os.Lstat(lockPath); err == nil && info.Mode().IsRegular() {
		paths = append(paths, lockPath)
	}
	return paths
}

func readRuntimeGenerationManifest(path string) (runtimeGenerationManifest, bool) {
	var manifest runtimeGenerationManifest
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxRuntimeGenerationManifestBytes {
		return manifest, false
	}
	file, err := os.Open(path)
	if err != nil {
		return manifest, false
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxRuntimeGenerationManifestBytes+1))
	if err != nil || len(body) > maxRuntimeGenerationManifestBytes {
		return manifest, false
	}
	if err := json.Unmarshal(body, &manifest); err != nil || manifest.Version != 1 {
		return runtimeGenerationManifest{}, false
	}
	return manifest, true
}

// safeManagedRuntimeName admits a plain runtime binary basename. Hidden
// names (the installer's own scratch/bookkeeping files), path separators,
// traversal and the veil binary itself (owned by the uninstall binary step)
// are refused so a tampered manifest cannot widen the removal set.
func safeManagedRuntimeName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return false
	}
	if strings.ContainsAny(name, `/\`) || filepath.Base(name) != name {
		return false
	}
	return name != "veil"
}
