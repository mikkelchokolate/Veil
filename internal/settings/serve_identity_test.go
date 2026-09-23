package settings

import "testing"

func TestCheckProcessCanAdoptCaddyIdentityRejectsWebBasePathDrift(t *testing.T) {
	process := Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", WebBasePath: "/oldsecret/"}
	candidate := Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", WebBasePath: "/newsecret/"}
	err := CheckProcessCanAdoptCaddyIdentity(candidate, process)
	if err == nil || err.Error() != "webBasePath cannot change until veil repair rewrites VEIL_WEB_BASE_PATH and restarts the Panel" {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckProcessCanAdoptCaddyIdentityAllowsMatchingMount(t *testing.T) {
	process := Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", WebBasePath: "/oldsecret/"}
	candidate := Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", WebBasePath: "/oldsecret/"}
	if err := CheckProcessCanAdoptCaddyIdentity(candidate, process); err != nil {
		t.Fatal(err)
	}
}

func TestCheckProcessCanAdoptCaddyIdentitySkipsRootProcessMount(t *testing.T) {
	process := Settings{PanelListen: "127.0.0.1:47321"}
	candidate := Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", WebBasePath: "/panel-e2e/"}
	if err := CheckProcessCanAdoptCaddyIdentity(candidate, process); err != nil {
		t.Fatal(err)
	}
}

func TestCheckProcessCanAdoptCaddyIdentityRejectsPanelAccessDrift(t *testing.T) {
	process := Settings{PanelAccess: "caddy", PanelListen: "127.0.0.1:2096", WebBasePath: "/oldsecret/"}
	candidate := Settings{PanelAccess: "direct", PanelListen: "127.0.0.1:2096", WebBasePath: "/oldsecret/"}
	err := CheckProcessCanAdoptCaddyIdentity(candidate, process)
	if err == nil || err.Error() != "panelAccess cannot change until veil repair rewrites veil.env and restarts the Panel" {
		t.Fatalf("err = %v", err)
	}
}

func TestSettingsValidationRejectsUnsupportedAcmeChallengeMode(t *testing.T) {
	for _, mode := range []string{"dns-01", "bogus", "tls-alpn"} {
		settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", AcmeChallengeMode: mode}
		err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
		if err == nil || err.Error() != "acmeChallengeMode must be http-01 or tls-alpn-01" {
			t.Errorf("mode %q: err = %v", mode, err)
		}
	}
}

func TestSettingsValidationAcceptsSupportedAcmeChallengeModes(t *testing.T) {
	// Only the two OpenAPI enum members are "supported modes" (#820).
	for _, mode := range []string{"http-01", "tls-alpn-01"} {
		settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", AcmeChallengeMode: mode}
		if err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{}); err != nil {
			t.Errorf("mode %q: %v", mode, err)
		}
		if settings.AcmeChallengeMode != mode {
			t.Errorf("mode %q was not persisted: %q", mode, settings.AcmeChallengeMode)
		}
	}
}

// TestSettingsValidationAcmeChallengeModeEmptyMeansUnset pins the deliberate
// contract: "" is not an OpenAPI enum member — it is the flat-field unset
// sentinel. acmeChallengeMode is a plain string field, so a PUT that omits
// the key decodes to "" and must not be rejected, or every settings update
// not carrying the field would fail. It persists as "" (unset) and the
// challenge planner applies its default (#820).
func TestSettingsValidationAcmeChallengeModeEmptyMeansUnset(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}
	if err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{}); err != nil {
		t.Fatalf("absent acmeChallengeMode must not error: %v", err)
	}
	if settings.AcmeChallengeMode != "" {
		t.Fatalf("absent acmeChallengeMode must persist as unset, got %q", settings.AcmeChallengeMode)
	}
}
