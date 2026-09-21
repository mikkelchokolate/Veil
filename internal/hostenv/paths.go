package hostenv

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultEtcDir is the packaged Veil configuration root.
const DefaultEtcDir = "/etc/veil"

// DefaultVarDir is the packaged Veil state root.
const DefaultVarDir = "/var/lib/veil"

// EtcDir resolves the Veil configuration root for the running process.
// VEIL_ETC_DIR wins; otherwise the parent of VEIL_LIVE_ROOT (installed units
// and generated material always set it to <etc>/generated), then the parent
// of VEIL_KEY_PATH, then the packaged default. Renderers, validators and the
// privileged helper must derive their managed /etc/veil paths from here so a
// custom --etc-dir install keeps every consumer in the same tree.
func EtcDir() string {
	if v := strings.TrimSpace(os.Getenv("VEIL_ETC_DIR")); v != "" {
		return filepath.Clean(v)
	}
	if v := strings.TrimSpace(os.Getenv("VEIL_LIVE_ROOT")); v != "" {
		if dir := filepath.Clean(filepath.Dir(v)); dir != "." && dir != string(filepath.Separator) {
			return dir
		}
	}
	if v := strings.TrimSpace(os.Getenv("VEIL_KEY_PATH")); v != "" {
		if dir := filepath.Clean(filepath.Dir(v)); dir != "." && dir != string(filepath.Separator) {
			return dir
		}
	}
	return DefaultEtcDir
}

// VarDir resolves the Veil state root for the running process: VEIL_VAR_DIR,
// then the parent of VEIL_STATE_PATH, then the packaged default.
func VarDir() string {
	if v := strings.TrimSpace(os.Getenv("VEIL_VAR_DIR")); v != "" {
		return filepath.Clean(v)
	}
	if v := strings.TrimSpace(os.Getenv("VEIL_STATE_PATH")); v != "" {
		if dir := filepath.Clean(filepath.Dir(v)); dir != "." && dir != string(filepath.Separator) {
			return dir
		}
	}
	return DefaultVarDir
}
