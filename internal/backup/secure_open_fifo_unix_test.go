//go:build unix

package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// #1254 F5: openBackupRegularNoFollow runs in the root helper over paths in
// the service-writable backups tree. A regular file swapped for a FIFO
// between a caller's stat and this open must fail fast instead of blocking
// the helper forever on open(2) waiting for a writer, and the descriptor
// must never be handed to a reader as if it were an empty regular file.

// openWithDeadline runs fn in a goroutine and fails the test if it does not
// return in time. A blocked open on a writer-less FIFO is released by
// opening the write side so the goroutine does not leak past the test.
func openWithDeadline(t *testing.T, fifo string, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		if w, err := os.OpenFile(fifo, os.O_WRONLY, 0); err == nil {
			_ = w.Close()
		}
		<-done
		t.Fatalf("open of FIFO %s blocked (missing O_NONBLOCK)", fifo)
		return nil
	}
}

func makeFIFO(t *testing.T) string {
	t.Helper()
	fifo := filepath.Join(t.TempDir(), "archive.veilbak")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}
	return fifo
}

func TestOpenBackupRegularNoFollowRejectsFIFOWithoutBlocking(t *testing.T) {
	fifo := makeFIFO(t)
	err := openWithDeadline(t, fifo, func() error {
		file, err := openBackupRegularNoFollow(fifo)
		if file != nil {
			_ = file.Close()
		}
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("openBackupRegularNoFollow(FIFO) err = %v, want not-a-regular-file refusal", err)
	}
}

func TestOpenBackupRegularNoFollowRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	file, err := openBackupRegularNoFollow(dir)
	if file != nil {
		_ = file.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("openBackupRegularNoFollow(dir) err = %v, want not-a-regular-file refusal", err)
	}
}

func TestOpenBackupRegularNoFollowReturnsBlockingRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openBackupRegularNoFollow(path)
	if err != nil {
		t.Fatalf("open regular file: %v", err)
	}
	defer file.Close()
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if flags&unix.O_NONBLOCK != 0 {
		t.Fatalf("returned descriptor still has O_NONBLOCK set (flags %#x)", flags)
	}
	data := make([]byte, 64)
	n, err := file.Read(data)
	if err != nil || string(data[:n]) != `{"schemaVersion":1}` {
		t.Fatalf("read = %q, %v", data[:n], err)
	}
}

func TestOpenBackupRegularNoFollowStillRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	file, err := openBackupRegularNoFollow(link)
	if file != nil {
		_ = file.Close()
	}
	if err == nil {
		t.Fatal("openBackupRegularNoFollow followed a symlinked leaf")
	}
}

// Caller-level: the archive metadata and schema probes run by the root
// helper over a FIFO-swapped leaf must error out promptly.
func TestBackupCallersRejectFIFOLeafWithoutBlocking(t *testing.T) {
	cases := map[string]func(string) error{
		"archiveFileMetadata": func(p string) error {
			_, err := archiveFileMetadata("archive.veilbak", p)
			return err
		},
		"rawStateSchemaVersionFile": func(p string) error {
			_, err := rawStateSchemaVersionFile(p)
			return err
		},
		"copySnapshotFile": func(p string) error {
			_, err := copySnapshotFile(p, filepath.Join(t.TempDir(), "copy"), 1<<20)
			return err
		},
		"prepareTarballFileWithOptions": func(p string) error {
			_, _, _, err := prepareTarballFileWithOptions(p, "", 1<<20, t.TempDir(), CryptoOptions{})
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			fifo := makeFIFO(t)
			if err := openWithDeadline(t, fifo, func() error { return call(fifo) }); err == nil {
				t.Fatalf("%s accepted a FIFO leaf", name)
			}
		})
	}
}
