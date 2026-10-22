package privileged

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

// PolicyEnvOverride is a VEIL_* variable from the process environment that
// moves a DefaultPolicy path away from the packaged layout.
// When Redacted is set, Value is a placeholder, not the env value (see
// mayHoldSecret on policyEnvKnobs).
type PolicyEnvOverride struct {
	Name     string
	Value    string
	Packaged string
	Redacted bool
}

// policyEnvKnobs lists every environment variable DefaultPolicy reads, either
// directly through pathFromEnv or through hostenv.EtcDir/VarDir, with its
// packaged value. The packaged helper unit exports VEIL_STATE_PATH,
// VEIL_KEY_PATH, VEIL_APPLY_ROOT and VEIL_LIVE_ROOT with exactly these values
// on a default install. TestPolicyEnvKnobsCoverEveryDefaultPolicyEnvRead keeps
// the list in sync with defaults.go.
//
// mayHoldSecret marks a knob whose name reads like a secret value
// (VEIL_BACKUP_PASSPHRASE is a *path* to the passphrase file). An operator
// who exported the passphrase itself by mistake must not have it echoed to
// stderr or the journal, so a value that is not an absolute path, or that
// contains blanks (passphrases often do, packaged paths never), is redacted
// without hinting at its length.
var policyEnvKnobs = []struct {
	name, packaged string
	mayHoldSecret  bool
}{
	{"VEIL_ETC_DIR", hostenv.DefaultEtcDir, false},
	{"VEIL_VAR_DIR", hostenv.DefaultVarDir, false},
	{"VEIL_STATE_PATH", filepath.Join(hostenv.DefaultVarDir, "state.json"), false},
	{"VEIL_KEY_PATH", filepath.Join(hostenv.DefaultEtcDir, "state.key"), false},
	{"VEIL_APPLY_ROOT", filepath.Join(hostenv.DefaultVarDir, "staging"), false},
	{"VEIL_LIVE_ROOT", filepath.Join(hostenv.DefaultEtcDir, "generated"), false},
	{"VEIL_BACKUP_PASSPHRASE", filepath.Join(hostenv.DefaultEtcDir, "backup.passphrase"), true},
	{"VEIL_BACKUP_ROOT", filepath.Join(hostenv.DefaultVarDir, "backups"), false},
	{"VEIL_BACKUP_SFTP_CONFIG", filepath.Join(hostenv.DefaultEtcDir, "backup-sftp.json"), false},
	{"VEIL_BACKUP_SFTP_STATUS", filepath.Join(hostenv.DefaultVarDir, "backup-sftp-status.json"), false},
	{"VEIL_BACKUP_SFTP_KNOWN_HOSTS", filepath.Join(hostenv.DefaultVarDir, "backup-sftp.known_hosts"), false},
	{"VEIL_BACKUP_SFTP_INSTALL_ID", filepath.Join(hostenv.DefaultVarDir, "backup-sftp.install-id"), false},
}

// PolicyEnvOverrides reports the VEIL_* variables in the current process
// environment that retarget the root helper's policy paths (state, key,
// staging/live roots, backup passphrase, backup root, SFTP config and
// known_hosts). The helper trusts these paths for key generation, backups and
// restore allowlists, so a poisoned root environment (`sudo -E`, an exported
// shell profile) would silently redirect them (issue #1263). Values are
// trimmed and cleaned the way DefaultPolicy and hostenv consume them, so a
// variable that restates the packaged path is not reported.
func PolicyEnvOverrides() []PolicyEnvOverride {
	var out []PolicyEnvOverride
	for _, knob := range policyEnvKnobs {
		value := strings.TrimSpace(os.Getenv(knob.name))
		if value == "" || filepath.Clean(value) == knob.packaged {
			continue
		}
		override := PolicyEnvOverride{Name: knob.name, Value: value, Packaged: knob.packaged}
		if knob.mayHoldSecret && (!filepath.IsAbs(value) || strings.ContainsAny(value, " \t")) {
			override.Value = "<redacted: not an absolute path without spaces>"
			override.Redacted = true
		}
		out = append(out, override)
	}
	return out
}

// PolicyEnvKnobNames returns the names of every VEIL_* variable that
// PolicyEnvOverrides inspects, in table order.
func PolicyEnvKnobNames() []string {
	names := make([]string, 0, len(policyEnvKnobs))
	for _, knob := range policyEnvKnobs {
		names = append(names, knob.name)
	}
	return names
}
