package managementstate

import (
	"errors"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestSecretPolicyTransformsAllKnownSecretFields(t *testing.T) {
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{
			NaivePassword:     "naive-secret",
			Hysteria2Password: "hy2-secret",
			ProtocolFields: map[string]any{
				"naivePassword":     "naive-pf-secret",
				"hysteria2Password": "hy2-pf-secret",
			},
		},
		Inbounds: []model.Inbound{{
			Name:       "naive",
			Password:   "inbound-secret",
			OlcrtcAuth: "olcrtc-inbound-secret",
			ProtocolFields: map[string]any{
				"naivePassword":     "naive-inbound-pf-secret",
				"hysteria2Password": "hy2-inbound-pf-secret",
			},
			Profiles: []model.ClientProfile{{
				Name:     "alice",
				Password: "profile-secret",
			}},
		}},
		Warp: model.WarpConfig{LicenseKey: "warp-license", PrivateKey: "warp-private"},
	}

	err := NewSecretPolicy().Transform(&snapshot, func(value string) (string, error) { return "secret:" + value, nil })
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	assertSecretTransformed(t, snapshot.Settings.NaivePassword, "naive-secret")
	assertSecretTransformed(t, snapshot.Settings.Hysteria2Password, "hy2-secret")
	assertSecretTransformed(t, snapshot.Settings.ProtocolFields["naivePassword"].(string), "naive-pf-secret")
	assertSecretTransformed(t, snapshot.Settings.ProtocolFields["hysteria2Password"].(string), "hy2-pf-secret")
	assertSecretTransformed(t, snapshot.Inbounds[0].Password, "inbound-secret")
	assertSecretTransformed(t, snapshot.Inbounds[0].ProtocolFields["naivePassword"].(string), "naive-inbound-pf-secret")
	assertSecretTransformed(t, snapshot.Inbounds[0].ProtocolFields["hysteria2Password"].(string), "hy2-inbound-pf-secret")
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
	if err := (NewSecretPolicy()).Transform(nil, func(v string) (string, error) { return v, nil }); err != nil {
		t.Fatalf("nil snapshot should return nil error, got %v", err)
	}
	snapshot := model.ManagementSnapshot{Settings: model.Settings{NaivePassword: "x"}}
	if err := (NewSecretPolicy()).Transform(&snapshot, nil); err != nil {
		t.Fatalf("nil transform should return nil error, got %v", err)
	}
}

func TestSecretPolicyPropagatesTransformError(t *testing.T) {
	snapshot := model.ManagementSnapshot{Settings: model.Settings{NaivePassword: "x"}}
	boom := errors.New("boom")
	err := NewSecretPolicy().Transform(&snapshot, func(v string) (string, error) {
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
		Settings: model.Settings{NaivePassword: "", Hysteria2Password: "hy2", ProtocolFields: map[string]any{"hysteria2Password": "hy2-pf"}},
		Inbounds: []model.Inbound{
			{Name: "a", Password: "p1", Profiles: []model.ClientProfile{{Password: "pp1"}, {Password: "pp2"}}, ProtocolFields: map[string]any{"naivePassword": "np1"}},
			{Name: "b", Password: "p2", OlcrtcAuth: "oa1", Profiles: []model.ClientProfile{{Password: ""}}, ProtocolFields: map[string]any{"hysteria2Password": "hy2-pf-2"}},
		},
		Warp: model.WarpConfig{LicenseKey: "lk", PrivateKey: ""},
	}

	err := NewSecretPolicy().Transform(&snapshot, func(value string) (string, error) {
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
	if snapshot.Settings.ProtocolFields["hysteria2Password"] != "x:hy2-pf" {
		t.Fatalf("settings protocolFields not transformed: %+v", snapshot.Settings.ProtocolFields)
	}
	if snapshot.Inbounds[0].Password != "x:p1" || snapshot.Inbounds[0].Profiles[1].Password != "x:pp2" {
		t.Fatalf("inbounds not transformed: %+v", snapshot.Inbounds)
	}
	if snapshot.Inbounds[0].ProtocolFields["naivePassword"] != "x:np1" {
		t.Fatalf("inbound protocolFields not transformed: %+v", snapshot.Inbounds[0].ProtocolFields)
	}
	if snapshot.Inbounds[1].ProtocolFields["hysteria2Password"] != "x:hy2-pf-2" {
		t.Fatalf("inbound protocolFields not transformed: %+v", snapshot.Inbounds[1].ProtocolFields)
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
		{"settings.ProtocolFields.naivePassword", model.ManagementSnapshot{Settings: model.Settings{ProtocolFields: map[string]any{"naivePassword": "err"}}}, "err"},
		{"inbound.Password", model.ManagementSnapshot{Inbounds: []model.Inbound{{Name: "a", Password: "err"}}}, "err"},
		{"inbound.ProtocolFields.naivePassword", model.ManagementSnapshot{Inbounds: []model.Inbound{{Name: "a", ProtocolFields: map[string]any{"naivePassword": "err"}}}}, "err"},
		{"profile.Password", model.ManagementSnapshot{Inbounds: []model.Inbound{{Name: "a", Profiles: []model.ClientProfile{{Password: "err"}}}}}, "err"},
		{"warp.LicenseKey", model.ManagementSnapshot{Warp: model.WarpConfig{LicenseKey: "err"}}, "err"},
		{"warp.PrivateKey", model.ManagementSnapshot{Warp: model.WarpConfig{PrivateKey: "err"}}, "err"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := NewSecretPolicy().Transform(&tc.snapshot, func(v string) (string, error) {
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
