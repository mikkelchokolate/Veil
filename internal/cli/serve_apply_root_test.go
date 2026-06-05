package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
)

func TestResolveServeApplyRootUsesFlagBeforeEnvironment(t *testing.T) {
	t.Setenv("VEIL_APPLY_ROOT", "/env/veil")

	got, source := serveflow.NewEnvironment().ApplyRoot("/flag/veil")

	if got != "/flag/veil" || source != "--apply-root" {
		t.Fatalf("expected flag apply root, got %q from %q", got, source)
	}
}

func TestResolveServeApplyRootFallsBackToEnvironment(t *testing.T) {
	t.Setenv("VEIL_APPLY_ROOT", "/env/veil")

	got, source := serveflow.NewEnvironment().ApplyRoot("")

	if got != "/env/veil" || source != "VEIL_APPLY_ROOT" {
		t.Fatalf("expected env apply root, got %q from %q", got, source)
	}
}

func TestResolveServeApplyRootDefaultsToStagingDirectory(t *testing.T) {
	t.Setenv("VEIL_APPLY_ROOT", "")

	got, source := serveflow.NewEnvironment().ApplyRoot("")

	expected := "/var/lib/veil/staging"
	if runtime.GOOS == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		expected = filepath.Join(pd, "Veil")
	}

	if got != expected || source != "default" {
		t.Fatalf("expected default apply root, got %q from %q", got, source)
	}
}

func TestResolveServeLiveRootUsesFlagEnvironmentAndDefault(t *testing.T) {
	env := serveflow.NewEnvironment()
	if got, source := env.LiveRoot("/flag/live"); got != "/flag/live" || source != "--live-root" {
		t.Fatalf("flag live root=%q source=%q", got, source)
	}
	t.Setenv("VEIL_LIVE_ROOT", "/env/live")
	if got, source := env.LiveRoot(""); got != "/env/live" || source != "VEIL_LIVE_ROOT" {
		t.Fatalf("env live root=%q source=%q", got, source)
	}
	t.Setenv("VEIL_LIVE_ROOT", "")
	got, source := env.LiveRoot("")
	expected := "/etc/veil/generated"
	if runtime.GOOS == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		expected = filepath.Join(pd, "Veil", "live")
	}
	if got != expected || source != "default" {
		t.Fatalf("default live root=%q source=%q", got, source)
	}
}
