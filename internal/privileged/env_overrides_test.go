package privileged

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

func clearPolicyEnv(t *testing.T) {
	t.Helper()
	for _, knob := range policyEnvKnobs {
		t.Setenv(knob.name, "")
	}
}

func TestPolicyEnvOverridesEmptyWithoutEnv(t *testing.T) {
	clearPolicyEnv(t)
	if got := PolicyEnvOverrides(); len(got) != 0 {
		t.Fatalf("no VEIL_* env must yield no overrides, got %+v", got)
	}
}

// The packaged helper unit exports VEIL_STATE_PATH/KEY_PATH/APPLY_ROOT/
// LIVE_ROOT with the packaged values; restating them (also with a trailing
// slash or surrounding space) must not warn on every helper start.
func TestPolicyEnvOverridesIgnorePackagedValues(t *testing.T) {
	clearPolicyEnv(t)
	for _, knob := range policyEnvKnobs {
		t.Setenv(knob.name, " "+knob.packaged+"/ ")
	}
	if got := PolicyEnvOverrides(); len(got) != 0 {
		t.Fatalf("packaged values must not be reported, got %+v", got)
	}
}

func TestPolicyEnvOverridesReportEachRetargetingVariable(t *testing.T) {
	for _, knob := range policyEnvKnobs {
		t.Run(knob.name, func(t *testing.T) {
			clearPolicyEnv(t)
			value := filepath.Join(t.TempDir(), "attacker")
			t.Setenv(knob.name, value)
			got := PolicyEnvOverrides()
			if len(got) != 1 || got[0].Name != knob.name || got[0].Value != value || got[0].Packaged != knob.packaged {
				t.Fatalf("want one override for %s=%s (packaged %s), got %+v", knob.name, value, knob.packaged, got)
			}
		})
	}
}

// Every knob in the table must really move DefaultPolicy, and the table must
// name every env var DefaultPolicy reads: a new pathFromEnv call in
// defaults.go without a table entry would retarget the helper silently.
func TestPolicyEnvKnobsCoverEveryDefaultPolicyEnvRead(t *testing.T) {
	src, err := os.ReadFile("defaults.go")
	if err != nil {
		t.Fatal(err)
	}
	read := map[string]bool{
		// hostenv.EtcDir / hostenv.VarDir inputs.
		"VEIL_ETC_DIR": true, "VEIL_VAR_DIR": true, "VEIL_LIVE_ROOT": true, "VEIL_KEY_PATH": true, "VEIL_STATE_PATH": true,
	}
	for _, m := range regexp.MustCompile(`pathFromEnv\("(VEIL_[A-Z_]+)"`).FindAllStringSubmatch(string(src), -1) {
		read[m[1]] = true
	}
	table := map[string]bool{}
	for _, knob := range policyEnvKnobs {
		table[knob.name] = true
	}
	var missing, extra []string
	for name := range read {
		if !table[name] {
			missing = append(missing, name)
		}
	}
	for name := range table {
		if !read[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("policyEnvKnobs out of sync with DefaultPolicy: missing %v, extra %v", missing, extra)
	}

	clearPolicyEnv(t)
	base := DefaultPolicy()
	for _, knob := range policyEnvKnobs {
		clearPolicyEnv(t)
		t.Setenv(knob.name, knob.packaged)
		if got := DefaultPolicy(); !samePolicyPaths(got, base) {
			t.Fatalf("%s=%s (packaged) changed DefaultPolicy: %+v vs %+v", knob.name, knob.packaged, policyPaths(got), policyPaths(base))
		}
		t.Setenv(knob.name, filepath.Join(t.TempDir(), "elsewhere"))
		if got := DefaultPolicy(); samePolicyPaths(got, base) {
			t.Fatalf("%s override did not move any DefaultPolicy path; drop it from the table or fix defaults.go", knob.name)
		}
	}
}

func policyPaths(p Policy) []string {
	paths := []string{p.StagingRoot, p.GeneratedRoot, p.StateRoot, p.StatePath, p.KeyPath, p.PanelCertDir,
		p.BackupPassphrasePath, p.BackupRoot, p.BackupSftpConfigPath, p.BackupSftpStatusPath,
		p.BackupSftpKnownHostsPath, p.BackupSftpInstallIDPath, p.UpdateRoot, p.FencePath}
	return append(paths, p.CertDirs...)
}

func samePolicyPaths(a, b Policy) bool {
	pa, pb := policyPaths(a), policyPaths(b)
	if len(pa) != len(pb) {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return false
		}
	}
	return true
}
