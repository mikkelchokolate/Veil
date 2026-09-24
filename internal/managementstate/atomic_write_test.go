package managementstate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestStoreSaveCleansTemporaryFileAfterRenameFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := NewStore(path, nil).Save(modelSnapshotForAtomicWriteTest()); err == nil {
		t.Fatal("expected rename failure")
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy temporary file remains: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(root, ".state.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}

func TestAtomicStoreWriteDoesNotRenameBeforeSync(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	syncErr := errors.New("sync failed")

	err := writeStoreFileAtomicWithSync(path, []byte("new state"), nil, func(*os.File) error {
		return syncErr
	})
	if !errors.Is(err, syncErr) {
		t.Fatalf("error=%v want %v", err, syncErr)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination was committed before sync: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(root, ".state.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}

// Issue #801: preservation failures must fail the save closed instead of
// silently publishing a state file with caller-owned (or default-mode)
// permissions that lock the service account out.
func TestStoreSaveFailsClosedWhenStatFails(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	store := NewStore(path, nil)
	if err := store.Save(modelSnapshotForAtomicWriteTest()); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	statErr := errors.New("stat denied")
	originalStat := statStoreFile
	statStoreFile = func(string) (os.FileInfo, error) { return nil, statErr }
	t.Cleanup(func() { statStoreFile = originalStat })

	err = store.Save(modelSnapshotForAtomicWriteTest())
	if !errors.Is(err, statErr) {
		t.Fatalf("Save error=%v, want stat failure %v", err, statErr)
	}
	current, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(current) != string(original) {
		t.Fatal("state file changed despite failed ownership stat")
	}
}

func TestAtomicStoreWriteFailsClosedWhenChownFails(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	chownErr := errors.New("chown denied")
	originalChown := chownFile
	chownFile = func(*os.File, int, int) error { return chownErr }
	t.Cleanup(func() { chownFile = originalChown })

	err := writeStoreFileAtomicWithSync(path, []byte("new state"),
		&fileInfo{uid: 1000, gid: 1000, mode: 0o600}, func(*os.File) error { return nil })
	if !errors.Is(err, chownErr) {
		t.Fatalf("error=%v, want chown failure %v", err, chownErr)
	}
	assertAtomicWriteLeftNoTrace(t, root, path)
}

func TestAtomicStoreWriteFailsClosedWhenChmodFails(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	chmodErr := errors.New("chmod denied")
	originalChmod := chmodFile
	chmodFile = func(*os.File, os.FileMode) error { return chmodErr }
	t.Cleanup(func() { chmodFile = originalChmod })

	err := writeStoreFileAtomicWithSync(path, []byte("new state"),
		&fileInfo{uid: -1, gid: -1, mode: 0o600}, func(*os.File) error { return nil })
	if !errors.Is(err, chmodErr) {
		t.Fatalf("error=%v, want chmod failure %v", err, chmodErr)
	}
	assertAtomicWriteLeftNoTrace(t, root, path)
}

func assertAtomicWriteLeftNoTrace(t *testing.T, root, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination was committed despite preservation failure: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(root, ".state.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}

func modelSnapshotForAtomicWriteTest() model.ManagementSnapshot {
	return model.ManagementSnapshot{Settings: model.Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}}
}
