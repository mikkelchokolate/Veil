package cli

import (
	"os"
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
