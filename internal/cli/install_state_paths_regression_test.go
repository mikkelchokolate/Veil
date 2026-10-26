package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/hostaccess"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/service"
)

// stubInstallStatePathSeams replaces the install seams with recorders so
// applyRURecommendedInstall never touches the host, and returns where the apply
// step was pointed.
func stubInstallStatePathSeams(t *testing.T) *installer.ApplyPaths {
	t.Helper()
	withMockedInstallRuntimes(t)
	oldApply, oldSystemd := installApplyFunc, installSystemdRunFunc
	oldExecutable, oldPrepareHost := installExecutableFunc, installPrepareHostFunc
	applied := installer.ApplyPaths{}
	installApplyFunc = func(_ installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		applied = paths
		return installer.ApplyResult{BackupID: "backup-1", WrittenFiles: []string{filepath.Join(paths.EtcDir, "veil.env")}}, nil
	}
	installSystemdRunFunc = func([]service.SystemdAction) error { return nil }
	installExecutableFunc = func() (string, error) { return "/usr/local/bin/veil", nil }
	installPrepareHostFunc = func(hostaccess.Paths) error { return nil }
	t.Cleanup(func() {
		installApplyFunc, installSystemdRunFunc = oldApply, oldSystemd
		installExecutableFunc, installPrepareHostFunc = oldExecutable, oldPrepareHost
	})
	return &applied
}

// runInstallForStatePaths runs the install entry point with a fixed profile.
func runInstallForStatePaths(t *testing.T, opts ruRecommendedInstallOptions) error {
	t.Helper()
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return applyRURecommendedInstall(cmd, installer.RURecommendedProfile{
		Domain: "example.com", Username: "veil", Password: "pw", WebBasePath: "/panel/",
	}, opts)
}

// treeSnapshot lists every path under dir, so a refusal can be proven to leave
// the trees untouched.
func treeSnapshot(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	sort.Strings(paths)
	return paths
}

func assertSameTree(t *testing.T, before, after []string) {
	t.Helper()
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("install touched the tree although it refused:\nbefore %v\nafter  %v", before, after)
	}
}

// seedInstallState seals a state file with keyPath, the way a rotated host looks.
func seedInstallState(t *testing.T, keyPath, statePath string) {
	t.Helper()
	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("create key %s: %v", keyPath, err)
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
		t.Fatalf("save state %s: %v", statePath, err)
	}
}

// TestInstallUsesARotatedStateKeyInsideTheTree keeps a documented
// `rotate-key --new-key-path` working across a re-install: the key sits inside
// the target etc tree, the state decrypts with it, and the rendered environment
// must keep that key instead of the packaged <etc>/state.key (#1498).
func TestInstallUsesARotatedStateKeyInsideTheTree(t *testing.T) {
	applied := stubInstallStatePathSeams(t)
	etcDir, varDir := t.TempDir(), t.TempDir()
	keyDir := filepath.Join(etcDir, "secrets dir")
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(keyDir, "rotated state.key")
	statePath := filepath.Join(varDir, "state.json")
	seedInstallState(t, keyPath, statePath)
	// Quoted the way `veilenv` writes a path with a space.
	env := "VEIL_KEY_PATH=\"" + keyPath + "\"\nVEIL_STATE_PATH=" + statePath + "\nVEIL_PANEL_ACCESS=local\n"
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte(env), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := runInstallForStatePaths(t, ruRecommendedInstallOptions{EtcDir: etcDir, VarDir: varDir}); err != nil {
		t.Fatalf("install over a rotated key inside the tree: %v", err)
	}
	if applied.KeyPath != keyPath {
		t.Fatalf("install rendered VEIL_KEY_PATH from %q, want the recorded %q", applied.KeyPath, keyPath)
	}
	if applied.StatePath != statePath {
		t.Fatalf("install rendered VEIL_STATE_PATH from %q, want %q", applied.StatePath, statePath)
	}
	if _, err := os.Stat(filepath.Join(etcDir, "state.key")); err == nil {
		t.Fatal("install created the packaged state.key next to the rotated one")
	}
}

// TestInstallRefusesAStateKeyOutsideTheTree pins the refusal: the recorded key
// lives outside the tree this install would write, so a fresh Panel there would
// leave the real state behind (#1498).
func TestInstallRefusesAStateKeyOutsideTheTree(t *testing.T) {
	stubInstallStatePathSeams(t)
	etcDir, varDir := t.TempDir(), t.TempDir()
	outside := filepath.Join(t.TempDir(), "state.key")
	seedInstallState(t, outside, filepath.Join(t.TempDir(), "state.json"))
	env := "VEIL_KEY_PATH=" + outside + "\nVEIL_PANEL_ACCESS=local\n"
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte(env), 0o640); err != nil {
		t.Fatal(err)
	}
	etcBefore, varBefore := treeSnapshot(t, etcDir), treeSnapshot(t, varDir)

	err := runInstallForStatePaths(t, ruRecommendedInstallOptions{EtcDir: etcDir, VarDir: varDir})
	if err == nil {
		t.Fatal("install accepted a state key outside the target tree")
	}
	for _, want := range []string{outside, "--etc-dir", "--reset-install-state"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v does not mention %q", err, want)
		}
	}
	assertSameTree(t, etcBefore, treeSnapshot(t, etcDir))
	assertSameTree(t, varBefore, treeSnapshot(t, varDir))
}

// TestInstallRefusesAStateFileOutsideTheTree covers the other half: the state
// file lives outside the target var tree, so install must not create a fresh
// default state next to it and report success.
func TestInstallRefusesAStateFileOutsideTheTree(t *testing.T) {
	stubInstallStatePathSeams(t)
	etcDir, varDir := t.TempDir(), t.TempDir()
	outside := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(outside, []byte("recorded state"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := "VEIL_STATE_PATH=" + outside + "\nVEIL_PANEL_ACCESS=local\n"
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte(env), 0o640); err != nil {
		t.Fatal(err)
	}
	etcBefore, varBefore := treeSnapshot(t, etcDir), treeSnapshot(t, varDir)

	err := runInstallForStatePaths(t, ruRecommendedInstallOptions{EtcDir: etcDir, VarDir: varDir})
	if err == nil {
		t.Fatal("install accepted a state file outside the target tree")
	}
	for _, want := range []string{outside, "--var-dir", "--reset-install-state"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v does not mention %q", err, want)
		}
	}
	assertSameTree(t, etcBefore, treeSnapshot(t, etcDir))
	assertSameTree(t, varBefore, treeSnapshot(t, varDir))
	if body, err := os.ReadFile(outside); err != nil || string(body) != "recorded state" {
		t.Fatalf("install touched the recorded state file: %q %v", body, err)
	}
}

// TestInstallResetFlagInstallsAgainstThePackagedLayout keeps the explicit
// override: the operator asks for a fresh panel on the packaged paths, and the
// recorded files stay untouched.
func TestInstallResetFlagInstallsAgainstThePackagedLayout(t *testing.T) {
	applied := stubInstallStatePathSeams(t)
	etcDir, varDir := t.TempDir(), t.TempDir()
	outside := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(outside, []byte("recorded state"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := "VEIL_KEY_PATH=" + filepath.Join(t.TempDir(), "state.key") + "\nVEIL_STATE_PATH=" + outside + "\nVEIL_PANEL_ACCESS=local\n"
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte(env), 0o640); err != nil {
		t.Fatal(err)
	}

	if err := runInstallForStatePaths(t, ruRecommendedInstallOptions{EtcDir: etcDir, VarDir: varDir, ResetInstallState: true}); err != nil {
		t.Fatalf("install with --reset-install-state: %v", err)
	}
	if want := filepath.Join(etcDir, "state.key"); applied.KeyPath != want {
		t.Fatalf("applied KeyPath = %q, want the packaged %q", applied.KeyPath, want)
	}
	if want := filepath.Join(varDir, "state.json"); applied.StatePath != want {
		t.Fatalf("applied StatePath = %q, want the packaged %q", applied.StatePath, want)
	}
	if body, err := os.ReadFile(outside); err != nil || string(body) != "recorded state" {
		t.Fatalf("install touched the recorded state file: %q %v", body, err)
	}
}

// TestInstallReadsTheCertSnapshotWithTheResolvedPair covers the LE IP-cert
// leg: the stand-alone HTTP-01 port is chosen from the persisted panel state, so
// that read has to use the same pair the rest of the install resolved (#1498).
func TestInstallReadsTheCertSnapshotWithTheResolvedPair(t *testing.T) {
	stubInstallStatePathSeams(t)
	oldSnapshot := installLoadCertSnapshot
	var gotState, gotKey string
	installLoadCertSnapshot = func(statePath, keyPath string) (model.ManagementSnapshot, bool) {
		gotState, gotKey = statePath, keyPath
		return model.ManagementSnapshot{}, false
	}
	t.Cleanup(func() { installLoadCertSnapshot = oldSnapshot })

	etcDir, varDir := t.TempDir(), t.TempDir()
	keyPath := filepath.Join(etcDir, "rotated-state.key")
	statePath := filepath.Join(varDir, "state.json")
	seedInstallState(t, keyPath, statePath)
	env := "VEIL_KEY_PATH=" + keyPath + "\nVEIL_STATE_PATH=" + statePath + "\nVEIL_PANEL_ACCESS=direct\n"
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte(env), 0o640); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := applyRURecommendedInstall(cmd, installer.RURecommendedProfile{
		Domain: "example.com", Username: "veil", Password: "pw", WebBasePath: "/panel/", PanelAccess: "direct",
	}, ruRecommendedInstallOptions{EtcDir: etcDir, VarDir: varDir, PanelAccess: "direct", LEIPCert: true, PublicIP: "127.0.0.1"})
	if err != nil {
		t.Fatalf("direct install: %v", err)
	}
	if gotState != statePath || gotKey != keyPath {
		t.Fatalf("cert snapshot read from (%q, %q), want the resolved (%q, %q)", gotState, gotKey, statePath, keyPath)
	}
}

// TestInstallUpdatesTheDomainOnARotatedStateKey pins the second LE call site:
// the issued certificate names the public IP and the persisted panel domain is
// pointed at it. Using the packaged key there failed with a GCM error that the
// caller downgraded to a warning, so the install reported success without the
// certificate identity it had just obtained.
func TestInstallUpdatesTheDomainOnARotatedStateKey(t *testing.T) {
	stubInstallStatePathSeams(t)
	oldIssue := leIPCertIssueFunc
	leIPCertIssueFunc = func(_ context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		if err := os.WriteFile(opts.CertPath, []byte("LE-CERT"), 0o644); err != nil {
			t.Fatalf("write fake cert: %v", err)
		}
		if err := os.WriteFile(opts.KeyPath, []byte("LE-KEY"), 0o640); err != nil {
			t.Fatalf("write fake key: %v", err)
		}
		return acmeip.IssuedCert{CertPath: opts.CertPath, KeyPath: opts.KeyPath}, nil
	}
	t.Cleanup(func() { leIPCertIssueFunc = oldIssue })

	etcDir, varDir := t.TempDir(), t.TempDir()
	keyPath := filepath.Join(etcDir, "rotated-state.key")
	statePath := filepath.Join(varDir, "state.json")
	seedInstallState(t, keyPath, statePath)
	// The pre-rotation key is still on disk, as the documented procedure leaves it.
	if _, err := secrets.LoadOrCreateKey(filepath.Join(etcDir, "state.key")); err != nil {
		t.Fatal(err)
	}
	env := "VEIL_KEY_PATH=" + keyPath + "\nVEIL_STATE_PATH=" + statePath + "\nVEIL_PANEL_ACCESS=direct\n"
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte(env), 0o640); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := applyRURecommendedInstall(cmd, installer.RURecommendedProfile{
		Domain: "example.com", Username: "veil", Password: "pw", WebBasePath: "/panel/", PanelAccess: "direct",
	}, ruRecommendedInstallOptions{EtcDir: etcDir, VarDir: varDir, PanelAccess: "direct", LEIPCert: true, PublicIP: "127.0.0.1"})
	if err != nil {
		t.Fatalf("direct install over a rotated key: %v", err)
	}
	if strings.Contains(out.String(), "could not obtain Let's Encrypt IP certificate") {
		t.Fatalf("install fell back to the self-signed certificate:\n%s", out.String())
	}
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := cipherFromRawKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok, err := managementstate.NewStore(statePath, cipher).Load()
	if err != nil || !ok {
		t.Fatalf("load state with the rotated key: ok=%v err=%v", ok, err)
	}
	if snapshot.Settings.Domain != "127.0.0.1" {
		t.Fatalf("panel domain = %q, want the issued IP identity", snapshot.Settings.Domain)
	}
}

// TestInstallRefusesAStateKeyLeafSymlinkedOutsideTheTree covers the leaf case:
// the recorded path is lexically inside the tree, but an existing symlink sends
// LoadOrCreateKey outside it, so install refuses instead of writing a key the
// environment does not name.
func TestInstallRefusesAStateKeyLeafSymlinkedOutsideTheTree(t *testing.T) {
	stubInstallStatePathSeams(t)
	etcDir, varDir := t.TempDir(), t.TempDir()
	outside := filepath.Join(t.TempDir(), "real.key")
	if _, err := secrets.LoadOrCreateKey(outside); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(etcDir, "state.key")
	if err := os.Symlink(outside, linked); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	env := "VEIL_KEY_PATH=" + linked + "\nVEIL_PANEL_ACCESS=local\n"
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte(env), 0o640); err != nil {
		t.Fatal(err)
	}
	etcBefore, varBefore := treeSnapshot(t, etcDir), treeSnapshot(t, varDir)

	err := runInstallForStatePaths(t, ruRecommendedInstallOptions{EtcDir: etcDir, VarDir: varDir})
	if err == nil {
		t.Fatal("install followed a state-key symlink out of the tree")
	}
	for _, want := range []string{linked, "--etc-dir", "--reset-install-state"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %v does not mention %q", err, want)
		}
	}
	assertSameTree(t, etcBefore, treeSnapshot(t, etcDir))
	assertSameTree(t, varBefore, treeSnapshot(t, varDir))
}
