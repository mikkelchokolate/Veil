package repair

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

// seedRotatedStateKey writes an installation whose veil.env names a rotated
// state key (what `veil admin rotate-key --new-key-path` leaves behind) and
// seals the state under that key. The pre-rotation key stays on disk, as the
// documented procedure tells operators to keep it.
func seedRotatedStateKey(t *testing.T, keepPrevious bool) (opts Options, rotatedKeyPath, statePath string) {
	t.Helper()
	etcDir := t.TempDir()
	varDir := t.TempDir()
	rotatedKeyPath = filepath.Join(etcDir, "rotated-state.key")
	statePath = filepath.Join(varDir, "state.json")

	key, err := secrets.LoadOrCreateKey(rotatedKeyPath)
	if err != nil {
		t.Fatalf("create rotated key: %v", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	snapshot := model.ManagementSnapshot{
		Users: []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin", TOTPSecret: "JBSWY3DPEHPK3PXP"}},
	}
	if err := managementstate.EncryptSnapshot(&snapshot, cipher); err != nil {
		t.Fatalf("encrypt snapshot: %v", err)
	}
	if err := managementstate.NewStore(statePath, cipher).Save(snapshot); err != nil {
		t.Fatalf("save state: %v", err)
	}
	if keepPrevious {
		if _, err := secrets.LoadOrCreateKey(filepath.Join(etcDir, "state.key")); err != nil {
			t.Fatalf("create previous key: %v", err)
		}
	}
	env := "VEIL_KEY_PATH=" + rotatedKeyPath + "\nVEIL_STATE_PATH=" + statePath + "\nVEIL_PANEL_ACCESS=local\n"
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte(env), 0o640); err != nil {
		t.Fatalf("write veil.env: %v", err)
	}
	opts = Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: t.TempDir()}
	return opts, rotatedKeyPath, statePath
}

// TestRepairReadsTheRotatedKeyFromVeilEnv locks #1485: with the pre-rotation key
// still on disk, repair used to decrypt the state with <etc>/state.key and abort
// with a bare GCM error before printing a plan.
func TestRepairReadsTheRotatedKeyFromVeilEnv(t *testing.T) {
	opts, rotatedKeyPath, statePath := seedRotatedStateKey(t, true)

	plan, err := buildRepairPlanFromOptions(opts)
	if err != nil {
		t.Fatalf("repair plan after a path-moving rotation: %v", err)
	}
	env := repairPlanEnvValue(plan, "VEIL_KEY_PATH")
	if env != rotatedKeyPath {
		t.Fatalf("planned VEIL_KEY_PATH = %q, want %q", env, rotatedKeyPath)
	}
	if got := repairPlanEnvValue(plan, "VEIL_STATE_PATH"); got != statePath {
		t.Fatalf("planned VEIL_STATE_PATH = %q, want %q", got, statePath)
	}
}

// TestRepairKeepsTheRotatedKeyWhenThePreviousKeyIsGone is the same host without
// <etc>/state.key: repair used to plan a veil.env rewrite pointing at a key file
// that does not exist, and the Panel could not load its state after the restart.
func TestRepairKeepsTheRotatedKeyWhenThePreviousKeyIsGone(t *testing.T) {
	opts, rotatedKeyPath, _ := seedRotatedStateKey(t, false)

	plan, err := buildRepairPlanFromOptions(opts)
	if err != nil {
		t.Fatalf("repair plan without the previous key: %v", err)
	}
	env := repairPlanEnvValue(plan, "VEIL_KEY_PATH")
	if env != rotatedKeyPath {
		t.Fatalf("planned VEIL_KEY_PATH = %q, want the live key %q", env, rotatedKeyPath)
	}
	if _, err := os.Stat(env); err != nil {
		t.Fatalf("repair would repoint VEIL_KEY_PATH at a missing key: %v", err)
	}
}

// TestRepairFallsBackToThePackagedKeyWithoutVeilEnv keeps the documented
// fallback: no installed environment means the packaged <etc>/state.key and
// <var>/state.json, exactly as before.
func TestRepairFallsBackToThePackagedKeyWithoutVeilEnv(t *testing.T) {
	opts := Options{Profile: "ru-recommended", EtcDir: t.TempDir(), VarDir: t.TempDir(), SystemdDir: t.TempDir()}

	plan, err := buildRepairPlanFromOptions(opts)
	if err != nil {
		t.Fatalf("repair plan without veil.env: %v", err)
	}
	if got, want := repairPlanEnvValue(plan, "VEIL_KEY_PATH"), filepath.Join(opts.EtcDir, "state.key"); got != want {
		t.Fatalf("planned VEIL_KEY_PATH = %q, want %q", got, want)
	}
	if got, want := repairPlanEnvValue(plan, "VEIL_STATE_PATH"), filepath.Join(opts.VarDir, "state.json"); got != want {
		t.Fatalf("planned VEIL_STATE_PATH = %q, want %q", got, want)
	}
}

// TestInstalledStatePathsPreferTheEnvironmentFile locks the resolution order:
// the installed veil.env outranks the process environment, which is only a
// fallback — the same order the rest of the root CLI uses.
func TestInstalledStatePathsPreferTheEnvironmentFile(t *testing.T) {
	opts, rotatedKeyPath, statePath := seedRotatedStateKey(t, true)
	t.Setenv("VEIL_KEY_PATH", filepath.Join(t.TempDir(), "process.key"))
	t.Setenv("VEIL_STATE_PATH", filepath.Join(t.TempDir(), "process.json"))

	gotState, gotKey := installedStatePaths(opts)
	if gotKey != rotatedKeyPath || gotState != statePath {
		t.Fatalf("installedStatePaths = (%q, %q), want (%q, %q)", gotState, gotKey, statePath, rotatedKeyPath)
	}
}

// TestInstalledStatePathsFallBackToProcessEnvironment covers the documented
// chain without an installed file: exported paths first, packaged defaults last.
func TestInstalledStatePathsFallBackToProcessEnvironment(t *testing.T) {
	etcDir := t.TempDir()
	varDir := t.TempDir()
	opts := Options{EtcDir: etcDir, VarDir: varDir}
	processKey := filepath.Join(t.TempDir(), "process.key")
	processState := filepath.Join(t.TempDir(), "process.json")
	t.Setenv("VEIL_KEY_PATH", processKey)
	t.Setenv("VEIL_STATE_PATH", processState)
	if gotState, gotKey := installedStatePaths(opts); gotKey != processKey || gotState != processState {
		t.Fatalf("installedStatePaths = (%q, %q), want the exported (%q, %q)", gotState, gotKey, processState, processKey)
	}

	t.Setenv("VEIL_KEY_PATH", "")
	t.Setenv("VEIL_STATE_PATH", "")
	wantState := filepath.Join(varDir, "state.json")
	wantKey := filepath.Join(etcDir, "state.key")
	if gotState, gotKey := installedStatePaths(opts); gotKey != wantKey || gotState != wantState {
		t.Fatalf("installedStatePaths = (%q, %q), want the packaged (%q, %q)", gotState, gotKey, wantState, wantKey)
	}
}

// TestRepairReadsAQuotedKeyPathFromVeilEnv locks the round-trip with the writer
// `rotate-key --new-key-path` uses: a key path containing a space is written
// quoted (internal/veilenv quotes ' ', '#', quotes and backslashes), so repair
// has to unquote it instead of stat-ing a path that still carries the quotes.
func TestRepairReadsAQuotedKeyPathFromVeilEnv(t *testing.T) {
	etcDir := t.TempDir()
	varDir := t.TempDir()
	keyDir := filepath.Join(etcDir, "secrets dir")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(keyDir, "rotated state.key")
	statePath := filepath.Join(varDir, "state.json")

	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("create rotated key: %v", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	snapshot := model.ManagementSnapshot{
		Users: []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin", TOTPSecret: "JBSWY3DPEHPK3PXP"}},
	}
	if err := managementstate.EncryptSnapshot(&snapshot, cipher); err != nil {
		t.Fatalf("encrypt snapshot: %v", err)
	}
	if err := managementstate.NewStore(statePath, cipher).Save(snapshot); err != nil {
		t.Fatalf("save state: %v", err)
	}
	// Written the way veilenv writes it: the space makes the value quoted.
	env := "VEIL_KEY_PATH=\"" + keyPath + "\"\nVEIL_STATE_PATH=" + statePath + "\nVEIL_PANEL_ACCESS=local\n"
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte(env), 0o640); err != nil {
		t.Fatalf("write veil.env: %v", err)
	}
	opts := Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: t.TempDir()}

	gotState, gotKey := installedStatePaths(opts)
	if gotKey != keyPath {
		t.Fatalf("installedStatePaths key = %q, want the unquoted %q", gotKey, keyPath)
	}
	if gotState != statePath {
		t.Fatalf("installedStatePaths state = %q, want %q", gotState, statePath)
	}
	if _, err := buildRepairPlanFromOptions(opts); err != nil {
		t.Fatalf("repair plan with a quoted key path: %v", err)
	}
}
