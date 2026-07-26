//go:build linux && linuxintegration

package linuxintegration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/api"
	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/statecommit"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

const keyRotationPanelChildEnv = "VEIL_KEY_ROTATION_PANEL_CHILD"

func TestIntegrationPrivilegedKeyRotationRecoveryAcrossDurablePhases(t *testing.T) {
	if os.Getenv(keyRotationPanelChildEnv) == "1" {
		runKeyRotationPanelChild(t)
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("real key-rotation privilege boundary requires root")
	}
	panelUID, panelGID := requireVeilIdentity(t)
	panelBinary := copyCurrentTestBinary(t)

	for _, tc := range []struct {
		phase     string
		committed bool
	}{
		{phase: "prepared"},
		{phase: "key-published"},
		{phase: "state-published"},
		{phase: "sqlite-committed", committed: true},
	} {
		t.Run(tc.phase, func(t *testing.T) {
			fixture := newPrivilegedRotationFixture(t, panelUID, panelGID, tc.phase)
			var recoveryCalls atomic.Int32
			policy := privileged.DefaultPolicy()
			policy.StateRoot = fixture.varDir
			policy.StatePath = fixture.statePath
			policy.KeyPath = fixture.keyPath
			policy.StagingRoot = filepath.Join(fixture.varDir, "staging", "generated")
			policy.GeneratedRoot = filepath.Join(fixture.etcDir, "generated")
			policy.BackupRoot = filepath.Join(fixture.varDir, "backups")
			policy.UpdateRoot = filepath.Join(fixture.varDir, "updates")
			policy.BackupPassphrasePath = filepath.Join(fixture.etcDir, "backup.passphrase")

			config := privileged.DefaultProductionConfig(policy, "integration")
			config.RotateKeyWorkflow = func(context.Context) error {
				_, err := statecommit.RotateKeyInterruptedForIntegration(statecommit.RotateKeyOptions{
					StatePath:    fixture.statePath,
					KeyPath:      fixture.keyPath,
					DatabasePath: fixture.databasePath,
				}, tc.phase)
				return err
			}
			config.RecoverKeyRotationWorkflow = func(context.Context) error {
				recoveryCalls.Add(1)
				return statecommit.RecoverKeyRotation(statecommit.RecoverKeyRotationOptions{
					StatePath:    fixture.statePath,
					DatabasePath: fixture.databasePath,
				})
			}
			server := privileged.NewServer(privileged.NewLocalAdapter(policy, privileged.NewProductionExecutor(config)))
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				done <- server.ServeUnix(ctx, fixture.socketPath, uint32(panelUID), true)
			}()
			waitForPath(t, fixture.socketPath)
			if err := os.Chown(fixture.socketPath, 0, panelGID); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Errorf("helper shutdown: %v", err)
					}
				case <-time.After(2 * time.Second):
					t.Error("helper did not stop")
				}
			})

			err := privileged.NewSocketClient(fixture.socketPath).RotateKey(context.Background(), privileged.RotateKeyRequest{})
			if err == nil {
				t.Fatalf("phase %s rotation did not report interruption", tc.phase)
			}
			journalPath := statecommit.KeyRotationJournalPath(fixture.statePath)
			journal := readAndAssertRootRotationArtifacts(t, journalPath)

			liveKeyBeforeRecovery := mustRead(t, fixture.keyPath)
			liveStateBeforeRecovery := mustRead(t, fixture.statePath)
			if tc.committed && (bytes.Equal(liveKeyBeforeRecovery, fixture.oldKey) || bytes.Equal(liveStateBeforeRecovery, fixture.oldState)) {
				t.Fatal("committed interruption did not publish the intended pair")
			}

			runPanelChild(t, panelBinary, panelUID, panelGID, fixture, journalPath)
			if recoveryCalls.Load() < 1 {
				t.Fatal("Panel startup did not invoke recover_key_rotation through helper")
			}
			if _, err := os.Stat(journalPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("recovery journal still present: %v", err)
			}

			wantRevision := uint64(0)
			wantKey, wantState := fixture.oldKey, fixture.oldState
			if tc.committed {
				wantRevision = 1
				wantKey, wantState = liveKeyBeforeRecovery, liveStateBeforeRecovery
			}
			if got := mustRead(t, fixture.keyPath); !bytes.Equal(got, wantKey) {
				t.Fatalf("phase %s recovered wrong key bytes", tc.phase)
			}
			if got := mustRead(t, fixture.statePath); !bytes.Equal(got, wantState) {
				t.Fatalf("phase %s recovered wrong state bytes", tc.phase)
			}
			assertRecoveredStateAndRevision(t, fixture, wantRevision)
			assertJournalPathsRemoved(t, journal)
		})
	}
}

type privilegedRotationFixture struct {
	root           string
	etcDir         string
	varDir         string
	statePath      string
	keyPath        string
	databasePath   string
	socketPath     string
	oldKey         []byte
	oldState       []byte
	expectedDomain string
}

func newPrivilegedRotationFixture(t *testing.T, panelUID, panelGID int, phase string) privilegedRotationFixture {
	t.Helper()
	root, err := os.MkdirTemp("", "veil-privileged-rotation-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove privilege fixture: %v", err)
		}
	})
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "var", "lib", "veil")
	runDir := filepath.Join(root, "run", "veil")
	for _, dir := range []string{etcDir, varDir, runDir} {
		if err := os.MkdirAll(dir, 0o770); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(dir, 0, panelGID); err != nil {
			t.Fatal(err)
		}
	}
	for _, parent := range []string{
		filepath.Join(root, "etc"),
		filepath.Join(root, "var"),
		filepath.Join(root, "var", "lib"),
		filepath.Join(root, "run"),
	} {
		if err := os.Chmod(parent, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(etcDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(varDir, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(runDir, 0o770); err != nil {
		t.Fatal(err)
	}

	keyPath := filepath.Join(etcDir, "state.key")
	statePath := filepath.Join(varDir, "state.json")
	databasePath := filepath.Join(varDir, "veil.db")
	var key [secrets.KeySize]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, key[:], 0o640); err != nil {
		t.Fatal(err)
	}
	defaults := managementstate.BuildDefaultState(managementstate.DefaultInput{
		Mode: "dev", Domain: "privileged-" + phase + ".example.test",
	})
	defaults.Settings.NaivePassword = "root-boundary-secret"
	snapshot := managementstate.BuildSnapshot(managementstate.SnapshotInput{
		Settings: defaults.Settings, Inbounds: defaults.Inbounds, Rules: defaults.Rules, Warp: defaults.Warp,
	})
	if err := managementstate.NewStore(statePath, cipher).Save(snapshot); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct {
		path string
		mode os.FileMode
	}{
		{keyPath, 0o640}, {statePath, 0o640}, {databasePath, 0o660},
	} {
		if err := os.Chown(file.path, 0, panelGID); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(file.path, file.mode); err != nil {
			t.Fatal(err)
		}
	}
	return privilegedRotationFixture{
		root: root, etcDir: etcDir, varDir: varDir, statePath: statePath, keyPath: keyPath,
		databasePath: databasePath, socketPath: filepath.Join(runDir, "helper.sock"),
		oldKey: append([]byte(nil), key[:]...), oldState: mustRead(t, statePath),
		expectedDomain: "privileged-" + phase + ".example.test",
	}
}

func runKeyRotationPanelChild(t *testing.T) {
	currentUser, err := user.Current()
	if err != nil || currentUser.Username != "veil" {
		t.Fatalf("Panel identity=%v err=%v, want veil", currentUser, err)
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	foundCapabilities := false
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "CapEff:") {
			foundCapabilities = true
			if strings.TrimSpace(strings.TrimPrefix(line, "CapEff:")) != "0000000000000000" {
				t.Fatalf("Panel has effective capabilities: %s", line)
			}
		}
	}
	if !foundCapabilities {
		t.Fatal("Panel process status did not report CapEff")
	}
	journalPath := os.Getenv("VEIL_TEST_JOURNAL_PATH")
	if _, err := os.ReadFile(journalPath); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("Panel read root-owned 0600 journal: %v", err)
	}
	etcDir := os.Getenv("VEIL_TEST_ETC_DIR")
	if err := os.WriteFile(filepath.Join(etcDir, "panel-must-not-write"), []byte("x"), 0o600); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("Panel wrote to read-only /etc/veil fixture: %v", err)
	}
	statePath := os.Getenv("VEIL_TEST_STATE_PATH")
	keyPath := os.Getenv("VEIL_TEST_KEY_PATH")
	handler, reloader := api.NewRouter(api.ServerInfo{
		Version: "integration", Mode: "dev", AuthToken: "integration-token",
		StatePath: statePath, KeyPath: keyPath,
		ApplyRoot:               filepath.Join(filepath.Dir(statePath), "staging"),
		LiveRoot:                filepath.Join(etcDir, "generated"),
		Privileged:              privileged.NewSocketClient(os.Getenv("VEIL_TEST_HELPER_SOCKET")),
		RequirePrivilegedHelper: true,
	})
	if closer, ok := reloader.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	request := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	request.Header.Set("Authorization", "Bearer integration-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("Panel startup settings status=%d body=%s", response.Code, response.Body.String())
	}
	var settings map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &settings); err != nil {
		t.Fatalf("decode Panel settings: %v", err)
	}
	if got, _ := settings["domain"].(string); got != os.Getenv("VEIL_TEST_EXPECTED_DOMAIN") {
		t.Fatalf("startup domain=%q, want recovered persisted state", got)
	}
}

func runPanelChild(t *testing.T, binary string, uid, gid int, fixture privilegedRotationFixture, journalPath string) {
	t.Helper()
	tmp := filepath.Join(fixture.varDir, "panel-tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(tmp, uid, gid); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-test.run=^TestIntegrationPrivilegedKeyRotationRecoveryAcrossDurablePhases$", "-test.count=1")
	command.Env = append(os.Environ(),
		keyRotationPanelChildEnv+"=1",
		"VEIL_TEST_JOURNAL_PATH="+journalPath,
		"VEIL_TEST_ETC_DIR="+fixture.etcDir,
		"VEIL_TEST_STATE_PATH="+fixture.statePath,
		"VEIL_TEST_KEY_PATH="+fixture.keyPath,
		"VEIL_TEST_HELPER_SOCKET="+fixture.socketPath,
		"VEIL_TEST_EXPECTED_DOMAIN="+fixture.expectedDomain,
		"HOME="+tmp,
		"TMPDIR="+tmp,
	)
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Panel child: %v\n%s", err, output)
	}
}

func readAndAssertRootRotationArtifacts(t *testing.T, journalPath string) map[string]any {
	t.Helper()
	info, err := os.Stat(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if stat.Uid != 0 || info.Mode().Perm() != 0o600 {
		t.Fatalf("journal owner/mode uid=%d mode=%#o, want root 0600", stat.Uid, info.Mode().Perm())
	}
	var journal map[string]any
	if err := json.Unmarshal(mustRead(t, journalPath), &journal); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"previousKeyPath", "intendedKeyPath", "previousStatePath", "intendedStatePath"} {
		path, _ := journal[field].(string)
		artifactInfo, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s artifact: %v", field, err)
		}
		if artifactInfo.Sys().(*syscall.Stat_t).Uid != 0 {
			t.Fatalf("%s is not root-owned", field)
		}
	}
	return journal
}

func assertRecoveredStateAndRevision(t *testing.T, fixture privilegedRotationFixture, wantRevision uint64) {
	t.Helper()
	key := mustRead(t, fixture.keyPath)
	if len(key) != secrets.KeySize {
		t.Fatalf("recovered key length=%d", len(key))
	}
	var keyArray [secrets.KeySize]byte
	copy(keyArray[:], key)
	cipher, err := secrets.NewCipher(keyArray)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok, err := managementstate.NewStore(fixture.statePath, cipher).Load()
	if err != nil || !ok {
		t.Fatalf("load recovered state ok=%v err=%v", ok, err)
	}
	if snapshot.Settings.Domain == "" {
		t.Fatal("recovered state lost settings")
	}
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	revisions, err := apply.NewRevisionStore(db).Get()
	if err != nil {
		t.Fatal(err)
	}
	if revisions.Desired != wantRevision {
		t.Fatalf("desired revision=%d, want %d", revisions.Desired, wantRevision)
	}
}

func assertJournalPathsRemoved(t *testing.T, journal map[string]any) {
	t.Helper()
	for _, field := range []string{"previousKeyPath", "intendedKeyPath", "previousStatePath", "intendedStatePath"} {
		path, _ := journal[field].(string)
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s cleanup: %v", field, err)
		}
	}
}

func requireVeilIdentity(t *testing.T) (int, int) {
	t.Helper()
	account, err := user.Lookup("veil")
	if err != nil {
		t.Fatalf("veil account is required by privilege-boundary CI: %v", err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		t.Fatal(err)
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		t.Fatal(err)
	}
	return uid, gid
}

func copyCurrentTestBinary(t *testing.T) string {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.CreateTemp("", "veil-linuxintegration-panel-test-*")
	if err != nil {
		t.Fatal(err)
	}
	destination := out.Name()
	t.Cleanup(func() { _ = os.Remove(destination) })
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	return destination
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return body
}
