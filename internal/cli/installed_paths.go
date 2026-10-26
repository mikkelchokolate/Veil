package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
	"github.com/mikkelchokolate/Veil/internal/veilenv"
	"github.com/spf13/cobra"
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

// keyPin is a generated drop-in that still hardcodes the state key path the
// pre-#1472 packages wrote instead of reading veil.env. Pinned is the path the
// unit actually uses, empty when it cannot be parsed; the caller needs it to
// tell a pin on the old path from a pin on the path being rotated.
type keyPin struct {
	DropIn string
	Pinned string
}

// staleKeyPins lists drop-ins that pin the key path outside veil.env: a live
// VEIL_KEY_PATH= assignment in the helper unit's Environment= (without an
// EnvironmentFile=, which outranks it and makes the literal harmless), or a
// --key-path flag in the backup unit's ExecStart. Those override veil.env, so a
// rotation that repoints the environment leaves the helper and the backup timer
// on the old key until `veil install`/`veil repair` re-renders them (#1473).
// Every directory is scanned: the packaged unit tree and the runtime one, whose
// drop-ins load later and override the packaged ones (#1484). Commented-out
// lines set nothing in systemd and are skipped (#1484).
func staleKeyPins(systemdDirs ...string) []keyPin {
	var pins []keyPin
	seen := make(map[string]bool, len(systemdDirs))
	for _, systemdDir := range systemdDirs {
		dir := filepath.Clean(strings.TrimSpace(systemdDir))
		if dir == "." || dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		for _, candidate := range []struct {
			unit string
			pin  func(body string) (string, bool)
		}{
			{"veil-helper.service", func(body string) (string, bool) {
				if hasEnvironmentFileDirective(body) {
					return "", false
				}
				return unitEnvironmentValue(body, "VEIL_KEY_PATH")
			}},
			{"veil-backup.service", func(body string) (string, bool) {
				return execStartFlagValue(body, "--key-path")
			}},
		} {
			matches, err := filepath.Glob(filepath.Join(dir, candidate.unit+".d", "*.conf"))
			if err != nil {
				continue
			}
			for _, path := range matches {
				body, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				if pinned, ok := candidate.pin(string(body)); ok {
					pins = append(pins, keyPin{DropIn: path, Pinned: pinned})
				}
			}
		}
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].DropIn < pins[j].DropIn })
	return pins
}

// resolveSystemdUnitDir picks the unit directory a command works on: an
// explicit --systemd-dir wins, then VEIL_SYSTEMD_DIR recorded by the installed
// environment file, then the value the command already had (its flag default,
// or a test seam) (#1505).
func resolveSystemdUnitDir(cmd *cobra.Command, envPath, flagValue, packaged string) string {
	if cmd != nil && cmd.Flags().Lookup("systemd-dir") != nil && cmd.Flags().Changed("systemd-dir") {
		return flagValue
	}
	if dir := installedSystemdDir(envPath); dir != "" {
		return dir
	}
	if strings.TrimSpace(flagValue) == "" {
		return packaged
	}
	return flagValue
}

// installedSystemdDir returns the unit directory the installed environment
// records, or "" when it records none (or only the packaged default). A custom
// --systemd-dir install keeps its units outside /etc/systemd/system, so repair
// and the rotate-key preflight must not fall back to the packaged path (#1484).
func installedSystemdDir(envPath string) string {
	body, err := os.ReadFile(envPath)
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(veilenv.Value(body, veilenv.SystemdDirKey))
	if dir == "" || sameKeyPath(dir, veilenv.DefaultSystemdDir) {
		return ""
	}
	return dir
}

// hasEnvironmentFileDirective reports whether the fragment makes systemd load an
// environment file, which wins over a literal Environment= line. An empty value
// resets the list and a `-`-prefixed path systemd cannot open supplies nothing,
// so neither of them makes the literal harmless (#1482 review).
func hasEnvironmentFileDirective(body string) bool {
	for _, entry := range unitDirectiveLines(body, "EnvironmentFile=") {
		entry = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(entry), "-"))
		path := trimUnitQuotes(entry)
		// systemd requires an absolute path here; a relative one is ignored at
		// load time, so a same-named file in the CLI's working directory must
		// not make the fragment look env-backed (#1496 review).
		if path == "" || !filepath.IsAbs(path) {
			continue
		}
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// unitEnvironmentValue returns the value of an Environment= assignment called
// name. It understands the `Environment=NAME=value`, `Environment="NAME=value"`
// and `Environment=NAME="value"` forms.
func unitEnvironmentValue(body, name string) (string, bool) {
	for _, entry := range unitDirectiveLines(body, "Environment=") {
		for _, assignment := range strings.Fields(trimUnitQuotes(strings.TrimSpace(entry))) {
			if value, ok := strings.CutPrefix(assignment, name+"="); ok {
				return trimUnitQuotes(value), true
			}
		}
	}
	return "", false
}

// execStartFlagValue returns the argument flag takes on the unit's ExecStart=
// lines, accepting both `--flag value` and `--flag=value`.
func execStartFlagValue(body, flag string) (string, bool) {
	for _, line := range unitDirectiveLines(body, "ExecStart=") {
		args := strings.Fields(trimUnitQuotes(strings.TrimSpace(line)))
		for i, arg := range args {
			if arg == flag && i+1 < len(args) {
				return trimUnitQuotes(args[i+1]), true
			}
			if value, ok := strings.CutPrefix(arg, flag+"="); ok {
				return trimUnitQuotes(value), true
			}
		}
	}
	return "", false
}

// unitDirectiveLines returns the values of the repeated directives with prefix,
// skipping blank and commented-out lines the way systemd does.
func unitDirectiveLines(body, prefix string) []string {
	var values []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if value, ok := strings.CutPrefix(trimmed, prefix); ok {
			values = append(values, value)
		}
	}
	return values
}

// trimUnitQuotes removes one layer of systemd quoting from value.
func trimUnitQuotes(value string) string {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}
	return value
}
