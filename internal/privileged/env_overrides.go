package privileged

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

// PolicyEnvOverride is a VEIL_* variable from the process environment that
// moves a DefaultPolicy path away from the packaged layout.
type PolicyEnvOverride struct {
	Name     string
	Value    string
	Packaged string
}

// policyEnvKnobs lists every environment variable DefaultPolicy reads, either
// directly through pathFromEnv or through hostenv.EtcDir/VarDir, with its
// packaged value. The packaged helper unit exports VEIL_STATE_PATH,
// VEIL_KEY_PATH, VEIL_APPLY_ROOT and VEIL_LIVE_ROOT with exactly these values
// on a default install. TestPolicyEnvKnobsCoverEveryDefaultPolicyEnvRead keeps
// the list in sync with defaults.go.
var policyEnvKnobs = []struct{ name, packaged string }{
	{"VEIL_ETC_DIR", hostenv.DefaultEtcDir},
	{"VEIL_VAR_DIR", hostenv.DefaultVarDir},
	{"VEIL_STATE_PATH", filepath.Join(hostenv.DefaultVarDir, "state.json")},
	{"VEIL_KEY_PATH", filepath.Join(hostenv.DefaultEtcDir, "state.key")},
	{"VEIL_APPLY_ROOT", filepath.Join(hostenv.DefaultVarDir, "staging")},
	{"VEIL_LIVE_ROOT", filepath.Join(hostenv.DefaultEtcDir, "generated")},
	{"VEIL_BACKUP_PASSPHRASE", filepath.Join(hostenv.DefaultEtcDir, "backup.passphrase")},
	{"VEIL_BACKUP_ROOT", filepath.Join(hostenv.DefaultVarDir, "backups")},
	{"VEIL_BACKUP_SFTP_CONFIG", filepath.Join(hostenv.DefaultEtcDir, "backup-sftp.json")},
	{"VEIL_BACKUP_SFTP_STATUS", filepath.Join(hostenv.DefaultVarDir, "backup-sftp-status.json")},
	{"VEIL_BACKUP_SFTP_KNOWN_HOSTS", filepath.Join(hostenv.DefaultVarDir, "backup-sftp.known_hosts")},
	{"VEIL_BACKUP_SFTP_INSTALL_ID", filepath.Join(hostenv.DefaultVarDir, "backup-sftp.install-id")},
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
		out = append(out, PolicyEnvOverride{Name: knob.name, Value: value, Packaged: knob.packaged})
	}
	return out
}
