package cli

import "testing"

func TestResolveServeApplyRootUsesFlagBeforeEnvironment(t *testing.T) {
	t.Setenv("VEIL_APPLY_ROOT", "/env/veil")

	got, source := resolveServeApplyRoot("/flag/veil")

	if got != "/flag/veil" || source != "--apply-root" {
		t.Fatalf("expected flag apply root, got %q from %q", got, source)
	}
}

func TestResolveServeApplyRootFallsBackToEnvironment(t *testing.T) {
	t.Setenv("VEIL_APPLY_ROOT", "/env/veil")

	got, source := resolveServeApplyRoot("")

	if got != "/env/veil" || source != "VEIL_APPLY_ROOT" {
		t.Fatalf("expected env apply root, got %q from %q", got, source)
	}
}

func TestResolveServeApplyRootDefaultsToEtcVeil(t *testing.T) {
	t.Setenv("VEIL_APPLY_ROOT", "")

	got, source := resolveServeApplyRoot("")

	if got != "/etc/veil" || source != "default" {
		t.Fatalf("expected default apply root, got %q from %q", got, source)
	}
}
