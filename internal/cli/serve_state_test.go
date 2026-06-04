package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
)

func TestResolveServeStatePathUsesFlagBeforeEnvironment(t *testing.T) {
	t.Setenv("VEIL_STATE_PATH", "/env/state.json")

	got, source := serveflow.NewEnvironment().StatePath("/flag/state.json")

	if got != "/flag/state.json" || source != "--state" {
		t.Fatalf("expected flag state path/source, got path=%q source=%q", got, source)
	}
}

func TestResolveServeStatePathUsesEnvironmentFallback(t *testing.T) {
	t.Setenv("VEIL_STATE_PATH", "/env/state.json")

	got, source := serveflow.NewEnvironment().StatePath("")

	if got != "/env/state.json" || source != "VEIL_STATE_PATH" {
		t.Fatalf("expected env state path/source, got path=%q source=%q", got, source)
	}
}

func TestResolveServeStatePathUsesDefaultWhenUnset(t *testing.T) {
	t.Setenv("VEIL_STATE_PATH", "")

	got, source := serveflow.NewEnvironment().StatePath("")

	expected := "/var/lib/veil/state.json"
	if runtime.GOOS == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		expected = filepath.Join(pd, "Veil", "state.json")
	}

	if got != expected || source != "default" {
		t.Fatalf("expected default state path/source, got path=%q source=%q", got, source)
	}
}
