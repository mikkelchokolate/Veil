package backup

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestBackupManifestStoreSaveCreatesDurablePrivateFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "manifest.json")
	want := Manifest{Entries: []Entry{{OriginalPath: "/etc/veil/state.json", BackupPath: "state.json", Size: 42}}}

	store := NewBackupManifestStore(path)
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest=%+v want %+v", got, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Fatalf("manifest mode=%04o want 0600", mode)
		}
	}
}
