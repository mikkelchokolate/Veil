package api

import "testing"

func TestStateSecretPolicyTransformsAllKnownSecretFields(t *testing.T) {
	snapshot := managementSnapshot{
		Settings: Settings{NaivePassword: "naive-secret", Hysteria2Password: "hy2-secret"},
		Inbounds: []Inbound{{
			Name:     "naive",
			Password: "inbound-secret",
			Profiles: []ClientProfile{{
				Name:     "alice",
				Password: "profile-secret",
			}},
		}},
		Warp: WarpConfig{LicenseKey: "warp-license", PrivateKey: "warp-private"},
	}

	stateSecretPolicy{}.Transform(&snapshot, func(value string) string { return "secret:" + value })

	assertSecretTransformed(t, snapshot.Settings.NaivePassword, "naive-secret")
	assertSecretTransformed(t, snapshot.Settings.Hysteria2Password, "hy2-secret")
	assertSecretTransformed(t, snapshot.Inbounds[0].Password, "inbound-secret")
	assertSecretTransformed(t, snapshot.Inbounds[0].Profiles[0].Password, "profile-secret")
	assertSecretTransformed(t, snapshot.Warp.LicenseKey, "warp-license")
	assertSecretTransformed(t, snapshot.Warp.PrivateKey, "warp-private")
}

func assertSecretTransformed(t *testing.T, got string, original string) {
	t.Helper()
	if got != "secret:"+original {
		t.Fatalf("secret field = %q, want %q", got, "secret:"+original)
	}
}
