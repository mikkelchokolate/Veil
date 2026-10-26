package acmeip

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/syspath"
)

// poisonPATH puts a shim with the given name first in PATH and returns the
// marker it touches when executed.
func poisonPATH(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	shim := "#!/bin/sh\ntouch " + marker + "\nexec /usr/bin/" + name + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
}

// TestDefaultSystemIgnoresAmbientPATH locks #1429: the IP-certificate issuer runs
// as root from install/repair, so `env`, `sh` and the rest must come from the
// trusted system directories, never from the invoking shell's PATH.
func TestDefaultSystemIgnoresAmbientPATH(t *testing.T) {
	for _, tool := range []string{"env", "sh"} {
		t.Run(tool, func(t *testing.T) {
			marker := poisonPATH(t, tool)
			path, err := defaultSystem{}.LookPath(tool)
			if err != nil {
				t.Fatalf("LookPath(%s): %v", tool, err)
			}
			if strings.HasPrefix(path, filepath.Dir(marker)) {
				t.Fatalf("LookPath(%s) = %q, resolved from the ambient PATH", tool, path)
			}
			// The wrapper issues every command through `env HOME=...`; run a
			// harmless one and prove the planted env/sh never executes.
			sys := homeDirSystem{System: defaultSystem{}, home: t.TempDir()}
			if err := sys.Run("true"); err != nil {
				t.Fatalf("homeDirSystem.Run(true): %v", err)
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatalf("the issuer executed a planted %s from the ambient PATH", tool)
			}
		})
	}
}

// TestDefaultSystemCombinedOutputScrubsChildPATH proves the child shell runs with
// the trusted PATH, so a nested command cannot fall back to an ambient binary.
func TestDefaultSystemCombinedOutputScrubsChildPATH(t *testing.T) {
	marker := poisonPATH(t, "sh")
	out, err := defaultSystem{}.CombinedOutput("sh", "-c", `printf '%s' "$PATH"`)
	if err != nil {
		t.Fatalf("CombinedOutput: %v", err)
	}
	if got := string(out); got != syspath.TrustedPATH() {
		t.Fatalf("child PATH = %q, want the trusted %q", got, syspath.TrustedPATH())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the child shell came from the ambient PATH")
	}
}

// TestHomeDirSystemKeepsTheHomeOverride keeps the #1170 contract while the binary
// resolution moves to syspath.
func TestHomeDirSystemKeepsTheHomeOverride(t *testing.T) {
	home := t.TempDir()
	out, err := homeDirSystem{System: defaultSystem{}, home: home}.CombinedOutput("sh", "-c", `printf '%s' "$HOME"`)
	if err != nil {
		t.Fatalf("CombinedOutput: %v", err)
	}
	if got := string(out); got != home {
		t.Fatalf("HOME = %q, want %q", got, home)
	}
}
