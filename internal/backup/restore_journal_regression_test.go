package backup

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/storage"
)

const restoreCrashHelperEnv = "VEIL_RESTORE_CRASH_HELPER"

type restoreTripleFixture struct {
	archive          string
	statePath        string
	keyPath          string
	databasePath     string
	oldState         []byte
	oldKey           []byte
	oldDatabase      []byte
	intendedState    []byte
	intendedKey      []byte
	intendedDatabase []byte
}

func TestRestoreRecoversSIGKILLAfterEveryFilePublication(t *testing.T) {
	for faultFile := 1; faultFile <= 3; faultFile++ {
		t.Run(fmt.Sprintf("after-file-%d", faultFile), func(t *testing.T) {
			fixture := prepareRestoreTripleFixture(t)
			runRestoreCrashHelper(t, fixture, faultFile)

			// A check-only restore is a harmless re-entry into the restore
			// subsystem. Recovery of any durable journal must happen first.
			if _, err := RestoreBackupFileWithOptions(
				fixture.archive,
				fixture.statePath,
				fixture.keyPath,
				"",
				RestoreOptions{DatabasePath: fixture.databasePath, CheckOnly: true},
			); err != nil {
				t.Fatalf("recover interrupted restore: %v", err)
			}

			state := classifyRestoreTriple(t, fixture)
			if state == "mixed" {
				state = classifyRestoreTripleWithFencingFloor(t, fixture, 41)
			}
			if state == "mixed" {
				t.Fatal("restore recovery left a mixed state.json/state.key/veil.db triple")
			} else if state == "intended" {
				db, err := storage.OpenExisting(fixture.databasePath)
				if err != nil {
					t.Fatal(err)
				}
				var generation uint64
				err = db.QueryRow(`SELECT generation FROM apply_lease WHERE id=1`).Scan(&generation)
				_ = db.Close()
				if err != nil || generation < 41 {
					t.Fatalf("restored fencing floor=%d err=%v, want >=41", generation, err)
				}
			}
		})
	}
}

func TestRestoreCrashProcess(t *testing.T) {
	if os.Getenv(restoreCrashHelperEnv) != "1" {
		return
	}
	faultFile, err := strconv.Atoi(os.Getenv("VEIL_RESTORE_FAULT_FILE"))
	if err != nil || faultFile < 1 || faultFile > 3 {
		os.Exit(82)
	}
	archive := os.Getenv("VEIL_RESTORE_ARCHIVE")
	statePath := os.Getenv("VEIL_RESTORE_STATE")
	keyPath := os.Getenv("VEIL_RESTORE_KEY")
	databasePath := os.Getenv("VEIL_RESTORE_DATABASE")
	marker := os.Getenv("VEIL_RESTORE_MARKER")

	originalRename := restoreRename
	defer func() { restoreRename = originalRename }()
	publications := 0
	restoreRename = func(oldPath, newPath string) error {
		if err := originalRename(oldPath, newPath); err != nil {
			return err
		}
		base := filepath.Base(oldPath)
		if strings.HasPrefix(base, ".veil-restore-") &&
			(newPath == statePath || newPath == keyPath || newPath == databasePath) {
			publications++
			if publications == faultFile {
				if err := os.WriteFile(marker, []byte("published"), 0o600); err != nil {
					os.Exit(83)
				}
				select {}
			}
		}
		return nil
	}
	_, _ = RestoreBackupFileWithOptions(
		archive,
		statePath,
		keyPath,
		"",
		RestoreOptions{DatabasePath: databasePath, FencingGeneration: 41, Now: func() time.Time {
			return time.Date(2026, time.July, 27, 13, 0, 0, 0, time.UTC)
		}},
	)
	os.Exit(84)
}

func runRestoreCrashHelper(t *testing.T, fixture restoreTripleFixture, faultFile int) {
	t.Helper()
	marker := filepath.Join(filepath.Dir(fixture.statePath), "restore-crash-marker")
	command := exec.Command(os.Args[0], "-test.run=^TestRestoreCrashProcess$")
	command.Env = append(os.Environ(),
		restoreCrashHelperEnv+"=1",
		"VEIL_RESTORE_FAULT_FILE="+strconv.Itoa(faultFile),
		"VEIL_RESTORE_ARCHIVE="+fixture.archive,
		"VEIL_RESTORE_STATE="+fixture.statePath,
		"VEIL_RESTORE_KEY="+fixture.keyPath,
		"VEIL_RESTORE_DATABASE="+fixture.databasePath,
		"VEIL_RESTORE_MARKER="+marker,
	)
	var output strings.Builder
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			_, _ = command.Process.Wait()
			t.Fatalf("restore helper did not publish file %d: %s", faultFile, output.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = command.Process.Wait()
}

func prepareRestoreTripleFixture(t *testing.T) restoreTripleFixture {
	t.Helper()
	sourceState, sourceKey := writeValidBackupSource(t)
	sourceDatabase := filepath.Join(filepath.Dir(sourceState), "veil.db")
	root := t.TempDir()
	archive := filepath.Join(root, "restore-source.tar.gz")
	if err := CreateBackupFileWithOptions(archive, sourceState, sourceKey, "", ArchiveOptions{
		DatabasePath: sourceDatabase,
		CreatedAt:    time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC),
		VeilVersion:  "test",
	}); err != nil {
		t.Fatal(err)
	}
	verified, err := inspectBackupFile(archive, "", DefaultMaxBackupBytes)
	if err != nil {
		t.Fatal(err)
	}
	intendedDatabase, err := os.ReadFile(verified.databasePath)
	if err != nil {
		verified.cleanup()
		t.Fatal(err)
	}
	fixture := restoreTripleFixture{
		archive:          archive,
		statePath:        filepath.Join(root, "target", "state.json"),
		keyPath:          filepath.Join(root, "target", "state.key"),
		databasePath:     filepath.Join(root, "target", "veil.db"),
		intendedState:    append([]byte(nil), verified.state...),
		intendedKey:      append([]byte(nil), verified.key...),
		intendedDatabase: intendedDatabase,
	}
	verified.cleanup()

	if err := os.MkdirAll(filepath.Dir(fixture.statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	fixture.oldState = []byte(`{"old":"state"}`)
	fixture.oldKey = []byte("old-key-material")
	if err := os.WriteFile(fixture.statePath, fixture.oldState, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.keyPath, fixture.oldKey, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO migration_markers(key, version, applied_at, details) VALUES ('old-restore-triple', 1, 1, '{}')`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// Normalize the same checkpoint boundary used at restore start, then pin
	// the exact old database bytes for all-or-nothing classification.
	if err := checkpointSQLiteRestoreBoundary(fixture.databasePath); err != nil {
		t.Fatal(err)
	}
	fixture.oldDatabase, err = os.ReadFile(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func classifyRestoreTripleWithFencingFloor(t *testing.T, fixture restoreTripleFixture, floor uint64) string {
	t.Helper()
	state, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(fixture.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(state) != sha256.Sum256(fixture.intendedState) || sha256.Sum256(key) != sha256.Sum256(fixture.intendedKey) {
		return "mixed"
	}
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		return "mixed"
	}
	defer db.Close()
	var generation uint64
	if err := db.QueryRow(`SELECT generation FROM apply_lease WHERE id=1`).Scan(&generation); err != nil || generation < floor {
		return "mixed"
	}
	return "intended"
}

func classifyRestoreTriple(t *testing.T, fixture restoreTripleFixture) string {
	t.Helper()
	actual := make([][]byte, 3)
	for i, path := range []string{fixture.statePath, fixture.keyPath, fixture.databasePath} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read restore member %s: %v", path, err)
		}
		actual[i] = body
	}
	old := [][]byte{fixture.oldState, fixture.oldKey, fixture.oldDatabase}
	intended := [][]byte{fixture.intendedState, fixture.intendedKey, fixture.intendedDatabase}
	allOld, allIntended := true, true
	for i := range actual {
		allOld = allOld && sha256.Sum256(actual[i]) == sha256.Sum256(old[i])
		allIntended = allIntended && sha256.Sum256(actual[i]) == sha256.Sum256(intended[i])
	}
	if allOld {
		return "old"
	}
	if allIntended {
		return "intended"
	}
	return "mixed"
}
