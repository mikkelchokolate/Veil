package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/veilenv"
)

// installStatePaths resolves the state key and state file an install works on.
// The installed veil.env outranks the packaged layout, the same order the rest
// of the root CLI uses (#1474/#1477/#1485): a documented
// `rotate-key --new-key-path` leaves the pair elsewhere, and re-running install
// must not repoint the Panel back at the pre-rotation key (#1498).
//
// A recorded path outside the tree this install writes to is refused instead of
// ignored: installing a fresh Panel next to the real state reports success while
// the operator's data lives somewhere else, which is the silent failure this
// check exists for. `--reset-install-state` is the explicit way to say "ignore
// what the previous environment recorded"; the recorded files stay on disk in
// every case.
func installStatePaths(opts ruRecommendedInstallOptions) (statePath, keyPath string, err error) {
	keyPath = filepath.Join(opts.EtcDir, "state.key")
	statePath = filepath.Join(opts.VarDir, "state.json")
	if opts.ResetInstallState {
		return statePath, keyPath, nil
	}
	// Read with the parser that pairs with the writer in internal/veilenv, which
	// quotes a path holding a space or a quote when rotate-key moves the key.
	var body []byte
	if raw, readErr := os.ReadFile(filepath.Join(opts.EtcDir, "veil.env")); readErr == nil {
		body = raw
	}
	recordedKey := strings.TrimSpace(veilenv.Value(body, veilenv.KeyPathKey))
	recordedState := strings.TrimSpace(veilenv.Value(body, "VEIL_STATE_PATH"))
	if recordedKey != "" && !sameKeyPath(recordedKey, keyPath) {
		if !installPathWithin(recordedKey, opts.EtcDir) {
			return "", "", fmt.Errorf("the installed environment records VEIL_KEY_PATH=%s, which is outside the install tree %s; re-run with --etc-dir for the tree that records it, or pass --reset-install-state to install a fresh panel against %s and leave that key untouched", recordedKey, opts.EtcDir, opts.EtcDir)
		}
		keyPath = recordedKey
	}
	if recordedState != "" && !sameKeyPath(recordedState, statePath) {
		if !installPathWithin(recordedState, opts.VarDir) {
			return "", "", fmt.Errorf("the installed environment records VEIL_STATE_PATH=%s, which is outside the install tree %s; re-run with --var-dir for the tree that holds it, or pass --reset-install-state to install a fresh panel against %s and leave that state untouched", recordedState, opts.VarDir, opts.VarDir)
		}
		statePath = recordedState
	}
	return statePath, keyPath, nil
}

// installPathWithin reports whether path is inside dir. It is deliberately
// lexical: the install tree may not exist yet, and a symlinked parent is the
// operator's own layout.
func installPathWithin(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
