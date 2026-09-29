package hostenv

import (
	"os"
	"strings"
)

// ReadEnvFile parses a KEY=value environment file (the veil.env format the
// installer writes and systemd consumes as EnvironmentFile). Blank lines,
// comments and malformed lines are skipped; the last occurrence of a key
// wins. A missing or unreadable file yields an empty map — callers decide
// whether absence is meaningful.
//
// The panel daemon already sees these values through the unit's
// EnvironmentFile= directive, but CLI invocations (`veil cert`, reinstall
// planning, repair) run in a bare shell and must read the file themselves
// so persisted choices still reach them (#1186/#1189).
func ReadEnvFile(path string) map[string]string {
	values := map[string]string{}
	body, err := os.ReadFile(path)
	if err != nil {
		return values
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values[key] = strings.TrimSpace(value)
	}
	return values
}

// EnvOrFile resolves a configuration knob that may come from the process
// environment (systemd EnvironmentFile for the daemon, an operator's shell
// for CLI commands) or from a persisted env file map returned by
// ReadEnvFile. A non-empty process-environment value wins so an explicit
// operator override beats the persisted default (#1189).
func EnvOrFile(fileValues map[string]string, key string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fileValues[key]
}
