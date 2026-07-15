package panel

import (
	"strings"
	"testing"
)

func TestSettingsReliabilityAbortsLateLoadsOnUserInput(t *testing.T) {
	actions := panelSettingsReliabilityJS()
	for _, want := range []string{
		`let settingsLoadController = null;`,
		`function invalidateSettingsLoad()`,
		`settingsLoadController.abort();`,
		`const controller = new AbortController();`,
		`signal: controller.signal`,
		`error.name === 'AbortError'`,
		`settingsFormForReliability.addEventListener('input', invalidateSettingsLoad);`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("settings load reliability missing %q", want)
		}
	}
}

func TestSettingsSaveDoesNotOverwriteNewerEdits(t *testing.T) {
	actions := panelSettingsReliabilityJS()
	for _, want := range []string{
		`invalidateSettingsLoad();
      settingsSaveInFlight = true;`,
		`const generation = ++settingsLoadGeneration;`,
		`window.cachedSettings = saved;`,
		`await applySettingsData(saved, generation);`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("settings save generation protection missing %q", want)
		}
	}
}

func TestGeneratedSettingsPasswordPublishesInputEvent(t *testing.T) {
	actions := panelSettingsActionsJS()
	if !strings.Contains(actions, `input.dispatchEvent(new Event('input', { bubbles: true }));`) {
		t.Fatal("generated settings password does not invalidate an outstanding settings load")
	}
}

func TestRenderedPanelMountsSettingsLoadGuardOnce(t *testing.T) {
	html := NewRenderer(NewSliceCatalog(nil).RenderSlots()).BaseHTML()
	for _, marker := range []string{
		`let settingsLoadController = null;`,
		`settingsFormForReliability.addEventListener('input', invalidateSettingsLoad);`,
	} {
		if got := strings.Count(html, marker); got != 1 {
			t.Fatalf("rendered settings reliability marker %q count = %d, want 1", marker, got)
		}
	}
}
