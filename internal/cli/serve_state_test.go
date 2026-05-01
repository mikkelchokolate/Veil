package cli

import "testing"

func TestResolveServeStatePathUsesFlagBeforeEnvironment(t *testing.T) {
	t.Setenv("VEIL_STATE_PATH", "/env/state.json")

	got, source := resolveServeStatePath("/flag/state.json")

	if got != "/flag/state.json" || source != "--state" {
		t.Fatalf("expected flag state path/source, got path=%q source=%q", got, source)
	}
}

func TestResolveServeStatePathUsesEnvironmentFallback(t *testing.T) {
	t.Setenv("VEIL_STATE_PATH", "/env/state.json")

	got, source := resolveServeStatePath("")

	if got != "/env/state.json" || source != "VEIL_STATE_PATH" {
		t.Fatalf("expected env state path/source, got path=%q source=%q", got, source)
	}
}

func TestResolveServeStatePathUsesDefaultWhenUnset(t *testing.T) {
	t.Setenv("VEIL_STATE_PATH", "")

	got, source := resolveServeStatePath("")

	if got != "/var/lib/veil/state.json" || source != "default" {
		t.Fatalf("expected default state path/source, got path=%q source=%q", got, source)
	}
}
