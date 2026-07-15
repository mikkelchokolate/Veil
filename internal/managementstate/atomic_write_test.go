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

func modelSnapshotForAtomicWriteTest() model.ManagementSnapshot {
	return model.ManagementSnapshot{Settings: model.Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}}
}
