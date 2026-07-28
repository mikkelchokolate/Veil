package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneRestoreSafetyFilesKeepsNewestAndRejectsHardLinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "state.key")
	var paths []string
	for i := 0; i < 4; i++ {
		path := target + ".pre-restore-20260728T12000" + string(rune('0'+i)) + "Z"
		if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(int64(i+1), 0)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	deleted, err := PruneRestoreSafetyFiles(target, "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 2 {
		t.Fatalf("deleted=%v", deleted)
	}
	for i, path := range paths {
		_, err := os.Stat(path)
		if i < 2 && !os.IsNotExist(err) {
			t.Fatalf("old safety file remains: %s", path)
		}
		if i >= 2 && err != nil {
			t.Fatalf("new safety file removed: %s: %v", path, err)
		}
	}

	hardTarget := target + ".pre-restore-hard"
	probe := filepath.Join(root, "probe")
	if err := os.WriteFile(probe, []byte("do-not-overwrite"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(probe, hardTarget); err != nil {
		t.Fatal(err)
	}
	if _, err := PruneRestoreSafetyFiles(target, "", "", 0); err == nil {
		t.Fatal("expected hard-linked safety path rejection")
	}
	body, err := os.ReadFile(probe)
	if err != nil || string(body) != "do-not-overwrite" {
		t.Fatalf("hard-link target changed: %q %v", body, err)
	}
}
