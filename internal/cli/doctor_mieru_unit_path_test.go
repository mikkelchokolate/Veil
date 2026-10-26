package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/syspath"
)

// TestMieruUnitProbeUsesTrustedSystemctl locks the second half of #1490:
// `veil doctor` runs as root, so the systemctl probe must come from the trusted
// system directories instead of the invoking shell's PATH.
func TestMieruUnitProbeUsesTrustedSystemctl(t *testing.T) {
	trusted := t.TempDir()
	ambient := t.TempDir()
	ambientMarker := filepath.Join(ambient, "ran")
	output := "LoadState=loaded\nBindReadOnlyPaths=" + renderer.MieruResolvConfPath + ":/etc/resolv.conf\n"
	trustedScript := "#!/bin/sh\nprintf '%s' '" + output + "'\n"
	if err := os.WriteFile(filepath.Join(trusted, "systemctl"), []byte(trustedScript), 0o755); err != nil {
		t.Fatal(err)
	}
	ambientScript := "#!/bin/sh\ntouch " + ambientMarker + "\n"
	if err := os.WriteFile(filepath.Join(ambient, "systemctl"), []byte(ambientScript), 0o755); err != nil {
		t.Fatal(err)
	}

	originalDirs := syspath.TrustedDirs
	syspath.TrustedDirs = append([]string{trusted}, originalDirs...)
	t.Cleanup(func() { syspath.TrustedDirs = originalDirs })
	t.Setenv("PATH", ambient+string(os.PathListSeparator)+os.Getenv("PATH"))

	if got := probeMieruUnitFallbackState(); got != mieruUnitFallbackInstalled {
		t.Fatalf("probe state = %v, want installed", got)
	}
	if _, err := os.Stat(ambientMarker); err == nil {
		t.Fatal("veil doctor executed a planted systemctl from the ambient PATH")
	}
}
