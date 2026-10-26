package hostenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/syspath"
)

// TestApplyQUICUDPBuffersIgnoresAmbientPATH locks #1490: install and repair run
// as root, so the live `sysctl -w` must be resolved from the trusted system
// directories, never from the invoking shell's PATH.
func TestApplyQUICUDPBuffersIgnoresAmbientPATH(t *testing.T) {
	trusted := t.TempDir()
	ambient := t.TempDir()
	trustedMarker := filepath.Join(trusted, "ran")
	ambientMarker := filepath.Join(ambient, "ran")
	writeFakeTool(t, filepath.Join(trusted, "sysctl"), "#!/bin/sh\ntouch "+trustedMarker+"\n")
	writeFakeTool(t, filepath.Join(ambient, "sysctl"), "#!/bin/sh\ntouch "+ambientMarker+"\n")

	originalDirs := syspath.TrustedDirs
	syspath.TrustedDirs = append([]string{trusted}, originalDirs...)
	t.Cleanup(func() { syspath.TrustedDirs = originalDirs })
	t.Setenv("PATH", ambient+string(os.PathListSeparator)+os.Getenv("PATH"))

	stubQUICForApply(t)
	if err := ApplyQUICUDPBuffers(); err != nil {
		t.Fatalf("ApplyQUICUDPBuffers: %v", err)
	}
	if _, err := os.Stat(trustedMarker); err != nil {
		t.Fatalf("the trusted sysctl was not executed: %v", err)
	}
	if _, err := os.Stat(ambientMarker); err == nil {
		t.Fatal("install/repair executed a planted sysctl from the ambient PATH")
	}
}

// stubQUICForApply makes ApplyQUICUDPBuffers take the live-tuning path on any
// host without touching /etc or /proc.
func stubQUICForApply(t *testing.T) {
	t.Helper()
	origEUID, origGOOS := quicGeteuid, quicRuntimeGOOS
	origRead, origWrite, origMkdir := quicReadFile, quicWriteFile, quicMkdirAll
	t.Cleanup(func() {
		quicGeteuid, quicRuntimeGOOS = origEUID, origGOOS
		quicReadFile, quicWriteFile, quicMkdirAll = origRead, origWrite, origMkdir
	})
	quicGeteuid = func() int { return 0 }
	quicRuntimeGOOS = func() string { return "linux" }
	quicReadFile = func(string) ([]byte, error) { return []byte("212992\n"), nil }
	quicWriteFile = func(string, []byte, os.FileMode) error { return nil }
	quicMkdirAll = func(string, os.FileMode) error { return nil }
}

// writeFakeTool writes an executable shell script at path.
func writeFakeTool(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
