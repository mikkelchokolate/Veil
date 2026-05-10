package managementstate

import (
	"testing"

	"github.com/veil-panel/veil/internal/model"
)

func TestSecretPolicyTransformsAllKnownSecretFields(t *testing.T) {
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{NaivePassword: "naive-secret", Hysteria2Password: "hy2-secret"},
		Inbounds: []model.Inbound{{
			Name:     "naive",
			Password: "inbound-secret",
			Profiles: []model.ClientProfile{{
				Name:     "alice",
				Password: "profile-secret",
			}},
		}},
		Warp: model.WarpConfig{LicenseKey: "warp-license", PrivateKey: "warp-private"},
	}

	SecretPolicy{}.Transform(&snapshot, func(value string) string { return "secret:" + value })

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
