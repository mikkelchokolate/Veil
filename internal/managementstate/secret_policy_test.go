package managementstate

import (
	"errors"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestSecretPolicyTransformsAllKnownSecretFields(t *testing.T) {
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{NaivePassword: "naive-secret", Hysteria2Password: "hy2-secret", OlcrtcAuth: "olcrtc-secret"},
		Inbounds: []model.Inbound{{
			Name:       "naive",
			Password:   "inbound-secret",
			OlcrtcAuth: "olcrtc-inbound-secret",
			Profiles: []model.ClientProfile{{
				Name:     "alice",
				Password: "profile-secret",
			}},
		}},
		Warp: model.WarpConfig{LicenseKey: "warp-license", PrivateKey: "warp-private"},
	}

	err := SecretPolicy{}.Transform(&snapshot, func(value string) (string, error) { return "secret:" + value, nil })
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	assertSecretTransformed(t, snapshot.Settings.NaivePassword, "naive-secret")
	assertSecretTransformed(t, snapshot.Settings.Hysteria2Password, "hy2-secret")
	assertSecretTransformed(t, snapshot.Settings.OlcrtcAuth, "olcrtc-secret")
	assertSecretTransformed(t, snapshot.Inbounds[0].Password, "inbound-secret")
	assertSecretTransformed(t, snapshot.Inbounds[0].OlcrtcAuth, "olcrtc-inbound-secret")
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

func TestSecretPolicyHandlesNilInputs(t *testing.T) {
	if err := (SecretPolicy{}).Transform(nil, func(v string) (string, error) { return v, nil }); err != nil {
		t.Fatalf("nil snapshot should return nil error, got %v", err)
	}
	snapshot := model.ManagementSnapshot{Settings: model.Settings{NaivePassword: "x"}}
	if err := (SecretPolicy{}).Transform(&snapshot, nil); err != nil {
		t.Fatalf("nil transform should return nil error, got %v", err)
	}
}

func TestSecretPolicyPropagatesTransformError(t *testing.T) {
	snapshot := model.ManagementSnapshot{Settings: model.Settings{NaivePassword: "x"}}
	boom := errors.New("boom")
	err := SecretPolicy{}.Transform(&snapshot, func(v string) (string, error) {
		if v == "x" {
			return "", boom
		}
		return v, nil
	})
	if err != boom {
		t.Fatalf("expected transform error, got %v", err)
	}
}

func TestSecretPolicyTransformsMultipleInboundsAndProfiles(t *testing.T) {
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{NaivePassword: "", Hysteria2Password: "hy2", OlcrtcAuth: "olcrtc"},
		Inbounds: []model.Inbound{
			{Name: "a", Password: "p1", Profiles: []model.ClientProfile{{Password: "pp1"}, {Password: "pp2"}}},
			{Name: "b", OlcrtcAuth: "oa1", Profiles: []model.ClientProfile{{Password: ""}}},
		},
		Warp: model.WarpConfig{LicenseKey: "lk", PrivateKey: ""},
	}

	err := SecretPolicy{}.Transform(&snapshot, func(value string) (string, error) {
		if value == "" {
			return "empty", nil
		}
		return "x:" + value, nil
	})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if snapshot.Settings.NaivePassword != "empty" || snapshot.Settings.Hysteria2Password != "x:hy2" {
		t.Fatalf("settings not transformed: %+v", snapshot.Settings)
	}
	if snapshot.Inbounds[0].Password != "x:p1" || snapshot.Inbounds[0].Profiles[1].Password != "x:pp2" {
		t.Fatalf("inbounds not transformed: %+v", snapshot.Inbounds)
	}
	if snapshot.Warp.LicenseKey != "x:lk" || snapshot.Warp.PrivateKey != "empty" {
		t.Fatalf("warp not transformed: %+v", snapshot.Warp)
	}
}

func TestSecretPolicyPropagatesErrorForEachSecretField(t *testing.T) {
	boom := errors.New("boom")

	cases := []struct {
		name     string
		snapshot model.ManagementSnapshot
		trigger  string
	}{
		{"settings.NaivePassword", model.ManagementSnapshot{Settings: model.Settings{NaivePassword: "err"}}, "err"},
		{"settings.Hysteria2Password", model.ManagementSnapshot{Settings: model.Settings{Hysteria2Password: "err"}}, "err"},
		{"settings.OlcrtcAuth", model.ManagementSnapshot{Settings: model.Settings{OlcrtcAuth: "err"}}, "err"},
		{"inbound.Password", model.ManagementSnapshot{Inbounds: []model.Inbound{{Name: "a", Password: "err"}}}, "err"},
		{"inbound.OlcrtcAuth", model.ManagementSnapshot{Inbounds: []model.Inbound{{Name: "a", OlcrtcAuth: "err"}}}, "err"},
		{"profile.Password", model.ManagementSnapshot{Inbounds: []model.Inbound{{Name: "a", Profiles: []model.ClientProfile{{Password: "err"}}}}}, "err"},
		{"warp.LicenseKey", model.ManagementSnapshot{Warp: model.WarpConfig{LicenseKey: "err"}}, "err"},
		{"warp.PrivateKey", model.ManagementSnapshot{Warp: model.WarpConfig{PrivateKey: "err"}}, "err"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (SecretPolicy{}).Transform(&tc.snapshot, func(v string) (string, error) {
				if v == tc.trigger {
					return "", boom
				}
				return v, nil
			})
			if err != boom {
				t.Fatalf("expected boom, got %v", err)
			}
		})
	}
}
