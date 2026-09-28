//go:build unix

package managementstate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestStoreSavePreservesOwnershipAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path, nil)
	if err := store.Save(model.ManagementSnapshot{Settings: model.Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Set non-default ownership and permissions on the existing file.
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	// Chown requires root; skip the UID/GID assertion when running as non-root.
	var wantUID, wantGID int = -1, -1
	if os.Getuid() == 0 {
		wantUID, wantGID = 0, 0
		if err := os.Chown(path, wantUID, wantGID); err != nil {
			t.Fatalf("Chown: %v", err)
		}
	}

	if err := store.Save(model.ManagementSnapshot{Settings: model.Settings{PanelListen: "127.0.0.1:31337", Mode: "dev"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 640", info.Mode().Perm())
	}
	if os.Getuid() == 0 {
		uid, gid := fileOwnerUID(info), fileOwnerGID(info)
		if uid < 0 || gid < 0 {
			t.Fatal("expected *syscall.Stat_t")
		}
		if uid != wantUID || gid != wantGID {
			t.Fatalf("owner = %d:%d, want %d:%d", uid, gid, wantUID, wantGID)
		}
	}

	loaded, ok, err := store.Load()
	if err != nil || !ok || loaded.Settings.PanelListen != "127.0.0.1:31337" {
		t.Fatalf("Load = %+v ok=%v err=%v", loaded, ok, err)
	}
}
