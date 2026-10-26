package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
	"github.com/mikkelchokolate/Veil/internal/veilenv"
)

// resolveInstalledPaths resolves the state and key paths the root CLI layer
// works on. The installed veil.env is the live configuration the Panel, the
// privileged helper and the backup timer read, so it outranks the process
// environment; an explicit flag still beats both (#1474, #1477). liveKeyPath is
// the key path the Panel itself will read, which is what a command that moves
// the key has to keep its writes in sync with.
func resolveInstalledPaths(flagState, flagKey string) (statePath, keyPath, liveKeyPath, envPath string) {
	env := serveflow.NewEnvironment()
	envPath = veilenv.Resolve(veilenv.DefaultSystemdDir)
	envKey := ""
	if body, err := os.ReadFile(envPath); err == nil {
		envKey = veilenv.Value(body, veilenv.KeyPathKey)
		if state := veilenv.Value(body, "VEIL_STATE_PATH"); state != "" && strings.TrimSpace(flagState) == "" {
			statePath = state
		}
	}
	if statePath == "" {
		statePath, _ = env.StatePath(flagState)
	}
	switch {
	case strings.TrimSpace(flagKey) != "":
		keyPath = flagKey
	case envKey != "":
		keyPath = envKey
	default:
		keyPath, _ = env.KeyPath("")
	}
	liveKeyPath = envKey
	if liveKeyPath == "" {
		liveKeyPath, _ = env.KeyPath("")
	}
	return statePath, keyPath, liveKeyPath, envPath
}

// envNamesKey reports whether the installed environment file exists and names
// exactly keyPath, i.e. the units genuinely read the key file at that path.
// Without that distinction a no-`veil.env` host — where liveKeyPath falls back
// to the process environment or the packaged default — would look "managed" and
// an ordinary in-place rotation would be refused (#1477 review).
func envNamesKey(envPath, keyPath string) bool {
	body, err := os.ReadFile(envPath)
	if err != nil {
		return false
	}
	recorded := veilenv.Value(body, veilenv.KeyPathKey)
	return recorded != "" && sameKeyPath(recorded, keyPath)
}

// staleKeyPinDropIns lists generated drop-ins that still hardcode the state key
// path the pre-#1472 packages wrote: a VEIL_KEY_PATH= literal in the helper
// unit's Environment= without an EnvironmentFile=, or a --key-path flag in the
// backup unit's ExecStart. Those override veil.env, so a rotation that repoints
// the environment leaves the helper and the backup timer on the old key until
// `veil install`/`veil repair` re-renders the drop-ins (#1473).
func staleKeyPinDropIns(systemdDir string) []string {
	var stale []string
	for _, candidate := range []struct {
		unit    string
		isStale func(body string) bool
	}{
		{"veil-helper.service", func(body string) bool {
			return strings.Contains(body, "VEIL_KEY_PATH=") && !strings.Contains(body, "EnvironmentFile=")
		}},
		{"veil-backup.service", func(body string) bool {
			for _, line := range strings.Split(body, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "ExecStart=") && strings.Contains(line, "--key-path") {
					return true
				}
			}
			return false
		}},
	} {
		matches, err := filepath.Glob(filepath.Join(systemdDir, candidate.unit+".d", "*.conf"))
		if err != nil {
			continue
		}
		for _, path := range matches {
			body, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if candidate.isStale(string(body)) {
				stale = append(stale, path)
			}
		}
	}
	sort.Strings(stale)
	return stale
}
