package olcrtc

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestPluginMetadata(t *testing.T) {
	p := New()
	if got, want := p.Protocol(), "olcrtc"; got != want {
		t.Errorf("Protocol() = %q, want %q", got, want)
	}
	if got, want := p.DisplayName(), "olcRTC"; got != want {
		t.Errorf("DisplayName() = %q, want %q", got, want)
	}
	if got, want := p.Transports(), []string{"udp"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Transports() = %v, want %v", got, want)
	}
	if p.RequiresCaddy() {
		t.Error("RequiresCaddy() should be false")
	}
	if got, want := p.FirewallService(), ""; got != want {
		t.Errorf("FirewallService() = %q, want %q", got, want)
	}
	if got, want := p.MaxEnabled(), 0; got != want {
		t.Errorf("MaxEnabled() = %d, want %d", got, want)
	}
}

func TestRenderConfigNoInbounds(t *testing.T) {
	p := New()
	arts, ok, err := p.RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{},
		Paths:    generatedconfig.NewPaths("/etc/veil"),
		Inbounds: nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for no inbounds")
	}
	if len(arts) != 0 {
		t.Errorf("expected no artifacts, got %d", len(arts))
	}
}

func TestRenderConfigWithInbounds(t *testing.T) {
	p := New()
	input := generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{},
		Paths:    generatedconfig.NewPaths("/etc/veil"),
		Inbounds: []model.Inbound{
			{
				Name:     "alpha",
				Protocol: "olcrtc",
				Password: "secret-key-used-for-render",
				ProtocolFields: map[string]any{
					"olcrtcAuth":      "telemost",
					"olcrtcTransport": "vp8channel",
					"olcrtcRoomID":    "room-alpha",
				},
			},
		},
	}

	arts, ok, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}

	wantPath := "/etc/veil/generated/olcrtc/alpha.yaml"
	if arts[0].Path != wantPath {
		t.Errorf("artifact path = %q, want %q", arts[0].Path, wantPath)
	}
	if arts[0].Body == "" {
		t.Fatal("expected non-empty rendered body")
	}
	for _, want := range []string{"provider: telemost", "transport: vp8channel", `id: "room-alpha"`, `key: "secret-key-used-for-render"`} {
		if !strings.Contains(arts[0].Body, want) {
			t.Errorf("rendered body missing %q; body:\n%s", want, arts[0].Body)
		}
	}
}

func TestRenderConfigFieldPrecedence(t *testing.T) {
	p := New()
	cases := []struct {
		name          string
		settings      model.Settings
		inbound       model.Inbound
		wantAuth      string
		wantTransport string
		wantRoom      string
	}{
		{
			name: "inbound protocolFields win",
			settings: model.Settings{
				ProtocolFields:  map[string]any{"olcrtcAuth": "wbstream", "olcrtcTransport": "seichannel", "olcrtcRoomID": "settings-room"},
				OlcrtcAuth:      "jitsi",
				OlcrtcTransport: "datachannel",
				OlcrtcRoomID:    "legacy-settings-room",
			},
			inbound: model.Inbound{
				Password:        "key",
				ProtocolFields:  map[string]any{"olcrtcAuth": "telemost", "olcrtcTransport": "vp8channel", "olcrtcRoomID": "inbound-room"},
				OlcrtcAuth:      "wbstream",
				OlcrtcTransport: "videochannel",
				OlcrtcRoomID:    "legacy-inbound-room",
			},
			wantAuth:      "telemost",
			wantTransport: "vp8channel",
			wantRoom:      "inbound-room",
		},
		{
			name: "inbound legacy fields fallback",
			settings: model.Settings{
				ProtocolFields: map[string]any{"olcrtcAuth": "wbstream", "olcrtcTransport": "seichannel", "olcrtcRoomID": "settings-room"},
			},
			inbound: model.Inbound{
				Password:        "key",
				OlcrtcAuth:      "telemost",
				OlcrtcTransport: "vp8channel",
				OlcrtcRoomID:    "legacy-inbound-room",
			},
			wantAuth:      "telemost",
			wantTransport: "vp8channel",
			wantRoom:      "legacy-inbound-room",
		},
		{
			name: "settings protocolFields fallback",
			settings: model.Settings{
				ProtocolFields: map[string]any{"olcrtcAuth": "wbstream", "olcrtcTransport": "seichannel", "olcrtcRoomID": "settings-room"},
			},
			inbound:       model.Inbound{Password: "key"},
			wantAuth:      "wbstream",
			wantTransport: "seichannel",
			wantRoom:      "settings-room",
		},
		{
			name: "settings legacy fields fallback",
			settings: model.Settings{
				OlcrtcAuth:      "wbstream",
				OlcrtcTransport: "videochannel",
				OlcrtcRoomID:    "legacy-settings-room",
			},
			inbound:       model.Inbound{Password: "key"},
			wantAuth:      "wbstream",
			wantTransport: "videochannel",
			wantRoom:      "legacy-settings-room",
		},
		{
			name:          "defaults when nothing set",
			settings:      model.Settings{},
			inbound:       model.Inbound{Password: "key"},
			wantAuth:      "jitsi",
			wantTransport: "datachannel",
			wantRoom:      "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := generatedconfig.ProtocolRenderInput{
				Settings: tc.settings,
				Paths:    generatedconfig.NewPaths("/etc/veil"),
				Inbounds: []model.Inbound{tc.inbound},
			}
			arts, ok, err := p.RenderConfig(input)
			if err != nil {
				t.Fatalf("RenderConfig error: %v", err)
			}
			if !ok {
				t.Fatal("expected ok=true")
			}
			body := arts[0].Body
			if !strings.Contains(body, "provider: "+tc.wantAuth) {
				t.Errorf("auth mismatch: want %q, body:\n%s", tc.wantAuth, body)
			}
			if !strings.Contains(body, "transport: "+tc.wantTransport) {
				t.Errorf("transport mismatch: want %q, body:\n%s", tc.wantTransport, body)
			}
			if !strings.Contains(body, `id: "`+tc.wantRoom+`"`) {
				t.Errorf("room mismatch: want %q, body:\n%s", tc.wantRoom, body)
			}
		})
	}
}

func TestArtifactSpec(t *testing.T) {
	p := New()
	spec := p.ArtifactSpec()
	if spec.Subpath != generatedconfig.OlcrtcConfigSubpath {
		t.Errorf("Subpath = %q, want %q", spec.Subpath, generatedconfig.OlcrtcConfigSubpath)
	}
	if spec.ValidationName != "olcrtc" {
		t.Errorf("ValidationName = %q, want %q", spec.ValidationName, "olcrtc")
	}
}

func TestRuntimeDescriptorsWithInbound(t *testing.T) {
	p := New()
	inbounds := []model.Inbound{
		{Name: "office", Protocol: "olcrtc"},
		{Name: "home", Protocol: "olcrtc"},
		{Name: "not-me", Protocol: "hysteria2"},
	}
	got := p.RuntimeDescriptors(inbounds)
	want := []service.ManagedRuntime{
		{
			Name:             "olcrtc-office",
			ActionName:       "olcrtc-office",
			Protocol:         "olcrtc",
			Transport:        "udp",
			Unit:             "veil-olcrtc@office.service",
			TemplateUnit:     templateUnit,
			PromotedSubpath:  "olcrtc/office.yaml",
			PromotedVerb:     "restart",
			ManualRestart:    true,
			HealthCheckAfter: true,
		},
		{
			Name:             "olcrtc-home",
			ActionName:       "olcrtc-home",
			Protocol:         "olcrtc",
			Transport:        "udp",
			Unit:             "veil-olcrtc@home.service",
			TemplateUnit:     templateUnit,
			PromotedSubpath:  "olcrtc/home.yaml",
			PromotedVerb:     "restart",
			ManualRestart:    true,
			HealthCheckAfter: true,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RuntimeDescriptors mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}

func TestRuntimeDescriptorsWithoutMatchingInbound(t *testing.T) {
	p := New()
	got := p.RuntimeDescriptors([]model.Inbound{{Name: "x", Protocol: "hysteria2"}})
	want := []service.ManagedRuntime{
		{
			Name:             "olcrtc",
			ActionName:       "olcrtc",
			Protocol:         "olcrtc",
			Transport:        "udp",
			Unit:             templateUnit,
			TemplateUnit:     templateUnit,
			PromotedSubpath:  "olcrtc/server.yaml",
			PromotedVerb:     "restart",
			ManualRestart:    true,
			HealthCheckAfter: true,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RuntimeDescriptors mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}

func TestRuntimeInstall(t *testing.T) {
	p := New()
	got := p.RuntimeInstall("amd64")
	want := runtimeinstall.Runtime{
		Name:          "olcrtc",
		Binary:        "olcrtc",
		Method:        runtimeinstall.MethodGoInstall,
		SourcePackage: "github.com/openlibrecommunity/olcrtc/cmd/olcrtc@latest",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RuntimeInstall mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}

func TestValidator(t *testing.T) {
	p := New()
	settings := model.Settings{}
	inbound := model.Inbound{}

	if err := p.ValidateSettings(settings, inbound); err != nil {
		t.Errorf("ValidateSettings returned error: %v", err)
	}
	if issues := p.ValidateInbound(settings, inbound); len(issues) != 0 {
		t.Errorf("ValidateInbound returned issues: %v", issues)
	}
	if p.NeedsDomain(settings, inbound) {
		t.Error("NeedsDomain should be false")
	}
	if p.HasCredential(settings, inbound) {
		t.Error("HasCredential should be false with empty password")
	}
	if !p.HasCredential(settings, model.Inbound{Password: "secret"}) {
		t.Error("HasCredential should be true with non-empty password")
	}
	if p.HasCredential(settings, model.Inbound{Password: "   "}) {
		t.Error("HasCredential should be false with whitespace-only password")
	}
}

func TestInboundFieldSchema(t *testing.T) {
	p := New()
	fields := p.InboundFieldSchema()
	if len(fields) != 3 {
		t.Fatalf("expected 3 inbound fields, got %d", len(fields))
	}
	wantKeys := []string{"olcrtcAuth", "olcrtcTransport", "olcrtcRoomID"}
	for i, key := range wantKeys {
		if fields[i].Key != key {
			t.Errorf("field[%d].Key = %q, want %q", i, fields[i].Key, key)
		}
		if fields[i].Scope != "inbound" {
			t.Errorf("field[%d].Scope = %q, want inbound", i, fields[i].Scope)
		}
	}
	if fields[0].Type != schema.FieldSelect {
		t.Errorf("olcrtcAuth type = %q, want select", fields[0].Type)
	}
	if fields[0].Default != "jitsi" {
		t.Errorf("olcrtcAuth default = %v, want jitsi", fields[0].Default)
	}
	if len(fields[0].Options) != 3 {
		t.Errorf("olcrtcAuth options = %d, want 3", len(fields[0].Options))
	}
	if fields[2].GenerateAction != "room" {
		t.Errorf("olcrtcRoomID generateAction = %q, want room", fields[2].GenerateAction)
	}
}

func TestSettingsFieldSchema(t *testing.T) {
	p := New()
	fields := p.SettingsFieldSchema()
	if len(fields) != 3 {
		t.Fatalf("expected 3 settings fields, got %d", len(fields))
	}
	for _, f := range fields {
		if f.Type != schema.FieldText {
			t.Errorf("field %q type = %q, want text", f.Key, f.Type)
		}
		if f.Scope != "settings" {
			t.Errorf("field %q scope = %q, want settings", f.Key, f.Scope)
		}
	}
}

func TestAutofillDefaults(t *testing.T) {
	p := New()
	inbound := model.Inbound{Name: "test", Protocol: "olcrtc"}
	out, err := p.Autofill(inbound)
	if err != nil {
		t.Fatalf("Autofill error: %v", err)
	}
	if out.ProtocolFields == nil {
		t.Fatal("expected ProtocolFields to be initialized")
	}
	if out.OlcrtcAuth != "jitsi" {
		t.Errorf("OlcrtcAuth = %q, want jitsi", out.OlcrtcAuth)
	}
	if out.OlcrtcTransport != "datachannel" {
		t.Errorf("OlcrtcTransport = %q, want datachannel", out.OlcrtcTransport)
	}
	if out.ProtocolFields["olcrtcAuth"] != "jitsi" {
		t.Errorf("ProtocolFields[olcrtcAuth] = %v, want jitsi", out.ProtocolFields["olcrtcAuth"])
	}
	if !isOlcrtcKey(out.Password) {
		t.Errorf("Password %q is not a 64-char hex key", out.Password)
	}
	if !strings.HasPrefix(out.OlcrtcRoomID, JitsiRoomBase) {
		t.Errorf("expected Jitsi room URL, got %q", out.OlcrtcRoomID)
	}
	if out.ProtocolFields["olcrtcRoomID"] != out.OlcrtcRoomID {
		t.Errorf("ProtocolFields room = %v, want %v", out.ProtocolFields["olcrtcRoomID"], out.OlcrtcRoomID)
	}
}

func TestAutofillPreservesExistingValues(t *testing.T) {
	p := New()
	key := strings.Repeat("a", 64)
	inbound := model.Inbound{
		Name:            "test",
		Protocol:        "olcrtc",
		Password:        key,
		OlcrtcAuth:      "telemost",
		OlcrtcTransport: "vp8channel",
		OlcrtcRoomID:    "manual-room",
		ProtocolFields: map[string]any{
			"olcrtcAuth":      "wbstream",
			"olcrtcTransport": "seichannel",
			"olcrtcRoomID":    "pf-room",
		},
	}
	out, err := p.Autofill(inbound)
	if err != nil {
		t.Fatalf("Autofill error: %v", err)
	}
	if out.OlcrtcAuth != "telemost" {
		t.Errorf("OlcrtcAuth = %q, want telemost", out.OlcrtcAuth)
	}
	if out.OlcrtcTransport != "vp8channel" {
		t.Errorf("OlcrtcTransport = %q, want vp8channel", out.OlcrtcTransport)
	}
	if out.Password != key {
		t.Errorf("Password changed to %q, want %q", out.Password, key)
	}
	if out.OlcrtcRoomID != "manual-room" {
		t.Errorf("OlcrtcRoomID = %q, want manual-room", out.OlcrtcRoomID)
	}
}

func TestAutofillReadsFromProtocolFields(t *testing.T) {
	p := New()
	inbound := model.Inbound{
		Name:     "test",
		Protocol: "olcrtc",
		ProtocolFields: map[string]any{
			"olcrtcAuth":      "wbstream",
			"olcrtcTransport": "videochannel",
		},
	}
	out, err := p.Autofill(inbound)
	if err != nil {
		t.Fatalf("Autofill error: %v", err)
	}
	if out.OlcrtcAuth != "wbstream" {
		t.Errorf("OlcrtcAuth = %q, want wbstream", out.OlcrtcAuth)
	}
	if out.OlcrtcTransport != "videochannel" {
		t.Errorf("OlcrtcTransport = %q, want videochannel", out.OlcrtcTransport)
	}
	if out.ProtocolFields["olcrtcAuth"] != "wbstream" {
		t.Errorf("ProtocolFields[olcrtcAuth] = %v", out.ProtocolFields["olcrtcAuth"])
	}
}

func TestBuildLinksNoProfiles(t *testing.T) {
	p := New()
	settings := model.Settings{Domain: "example.com"}
	inbound := model.Inbound{
		Name:      "alpha",
		Protocol:  "olcrtc",
		Transport: "udp",
		Port:      1234,
		Password:  "shared-key",
	}
	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("BuildLinks error: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	link := links[0]
	if link.Name != inbound.Name {
		t.Errorf("Name = %q, want %q", link.Name, inbound.Name)
	}
	wantURI := clientaccess.OlcrtcClientURI("jitsi", "datachannel", "", inbound.Password, "")
	if link.URI != wantURI {
		t.Errorf("URI = %q, want %q", link.URI, wantURI)
	}
}

func TestBuildLinksWithProfiles(t *testing.T) {
	p := New()
	settings := model.Settings{}
	inbound := model.Inbound{
		Name:      "alpha",
		Protocol:  "olcrtc",
		Transport: "udp",
		Port:      1234,
		Password:  "shared-key",
		ProtocolFields: map[string]any{
			"olcrtcAuth":      "telemost",
			"olcrtcTransport": "vp8channel",
			"olcrtcRoomID":    "room-1",
		},
		Profiles: []model.ClientProfile{
			{Name: "alice", Username: "alice", Password: "pw1", Enabled: true},
			{Name: "bob", Password: "pw2", Enabled: true},
			{Name: "carol", Username: "carol", Password: "pw3", Enabled: false},
		},
	}
	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("BuildLinks error: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}

	wantAlice := clientaccess.OlcrtcClientURI("telemost", "vp8channel", "room-1", inbound.Password, "alice")
	if links[0].Name != "alpha/alice" || links[0].URI != wantAlice {
		t.Errorf("alice link = %+v, want URI %q", links[0], wantAlice)
	}
	wantBob := clientaccess.OlcrtcClientURI("telemost", "vp8channel", "room-1", inbound.Password, "bob")
	if links[1].Name != "alpha/bob" || links[1].URI != wantBob {
		t.Errorf("bob link = %+v, want URI %q", links[1], wantBob)
	}
}

func TestBuildLinksProfileMissingPasswordErrors(t *testing.T) {
	p := New()
	inbound := model.Inbound{
		Name:     "alpha",
		Protocol: "olcrtc",
		Password: "shared-key",
		Profiles: []model.ClientProfile{
			{Name: "alice", Username: "alice", Enabled: true},
		},
	}
	if _, err := p.BuildLinks(model.Settings{}, inbound); err == nil {
		t.Error("expected error for profile missing password")
	}
}

func TestBuildLinksDefaultsForMissingAuthAndRoom(t *testing.T) {
	p := New()
	settings := model.Settings{}
	inbound := model.Inbound{
		Name:     "alpha",
		Protocol: "olcrtc",
		Password: "shared-key",
	}
	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("BuildLinks error: %v", err)
	}
	wantURI := clientaccess.OlcrtcClientURI("jitsi", "datachannel", "", "shared-key", "")
	if links[0].URI != wantURI {
		t.Errorf("URI = %q, want %q", links[0].URI, wantURI)
	}
}

func TestOlcrtcAuthFallbacks(t *testing.T) {
	settings := model.Settings{}
	inbound := model.Inbound{}
	if got := olcrtcAuth(settings, inbound); got != "jitsi" {
		t.Errorf("default auth = %q, want jitsi", got)
	}

	settings.OlcrtcAuth = "settings-legacy"
	if got := olcrtcAuth(settings, inbound); got != "settings-legacy" {
		t.Errorf("settings legacy auth = %q, want settings-legacy", got)
	}

	settings.ProtocolFields = map[string]any{"olcrtcAuth": "settings-pf"}
	if got := olcrtcAuth(settings, inbound); got != "settings-pf" {
		t.Errorf("settings protocolFields auth = %q, want settings-pf", got)
	}

	inbound.OlcrtcAuth = "inbound-legacy"
	if got := olcrtcAuth(settings, inbound); got != "inbound-legacy" {
		t.Errorf("inbound legacy auth = %q, want inbound-legacy", got)
	}

	inbound.ProtocolFields = map[string]any{"olcrtcAuth": "inbound-pf"}
	if got := olcrtcAuth(settings, inbound); got != "inbound-pf" {
		t.Errorf("inbound protocolFields auth = %q, want inbound-pf", got)
	}
}

func TestOlcrtcTransportFallbacks(t *testing.T) {
	settings := model.Settings{}
	inbound := model.Inbound{}
	if got := olcrtcTransport(settings, inbound); got != "datachannel" {
		t.Errorf("default transport = %q, want datachannel", got)
	}

	settings.OlcrtcTransport = "videochannel"
	if got := olcrtcTransport(settings, inbound); got != "videochannel" {
		t.Errorf("settings legacy transport = %q", got)
	}

	settings.ProtocolFields = map[string]any{"olcrtcTransport": "seichannel"}
	if got := olcrtcTransport(settings, inbound); got != "seichannel" {
		t.Errorf("settings protocolFields transport = %q", got)
	}

	inbound.OlcrtcTransport = "vp8channel"
	if got := olcrtcTransport(settings, inbound); got != "vp8channel" {
		t.Errorf("inbound legacy transport = %q", got)
	}

	inbound.ProtocolFields = map[string]any{"olcrtcTransport": "datachannel"}
	if got := olcrtcTransport(settings, inbound); got != "datachannel" {
		t.Errorf("inbound protocolFields transport = %q", got)
	}
}

func TestOlcrtcRoomIDFallbacks(t *testing.T) {
	settings := model.Settings{}
	inbound := model.Inbound{}
	if got := olcrtcRoomID(settings, inbound); got != "" {
		t.Errorf("default room = %q, want empty", got)
	}

	settings.OlcrtcRoomID = "settings-room"
	if got := olcrtcRoomID(settings, inbound); got != "settings-room" {
		t.Errorf("settings legacy room = %q", got)
	}

	settings.ProtocolFields = map[string]any{"olcrtcRoomID": "settings-pf-room"}
	if got := olcrtcRoomID(settings, inbound); got != "settings-pf-room" {
		t.Errorf("settings protocolFields room = %q", got)
	}

	inbound.OlcrtcRoomID = "inbound-room"
	if got := olcrtcRoomID(settings, inbound); got != "inbound-room" {
		t.Errorf("inbound legacy room = %q", got)
	}

	inbound.ProtocolFields = map[string]any{"olcrtcRoomID": "inbound-pf-room"}
	if got := olcrtcRoomID(settings, inbound); got != "inbound-pf-room" {
		t.Errorf("inbound protocolFields room = %q", got)
	}
}

func TestProtocolString(t *testing.T) {
	if got := protocolString(nil, "x", "fallback"); got != "fallback" {
		t.Errorf("nil map = %q, want fallback", got)
	}
	m := map[string]any{"x": "value", "y": 123, "z": "  spaced  "}
	if got := protocolString(m, "missing", "fallback"); got != "fallback" {
		t.Errorf("missing key = %q, want fallback", got)
	}
	if got := protocolString(m, "y", "fallback"); got != "fallback" {
		t.Errorf("non-string = %q, want fallback", got)
	}
	if got := protocolString(m, "z", "fallback"); got != "spaced" {
		t.Errorf("trimmed = %q, want spaced", got)
	}
}

func TestRenderConfigGeneratesKeyWhenMissing(t *testing.T) {
	p := New()
	input := generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{},
		Paths:    generatedconfig.NewPaths("/etc/veil"),
		Inbounds: []model.Inbound{{Name: "beta", Protocol: "olcrtc"}},
	}
	arts, ok, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig error: %v", err)
	}
	if !ok || len(arts) != 1 {
		t.Fatalf("expected one artifact, ok=%v, len=%d", ok, len(arts))
	}
	if !strings.Contains(arts[0].Body, "crypto:") {
		t.Errorf("rendered body missing crypto section:\n%s", arts[0].Body)
	}
}

func TestAutofillNonAutoProviderSkipsRoom(t *testing.T) {
	p := New()
	inbound := model.Inbound{
		Name:       "test",
		Protocol:   "olcrtc",
		OlcrtcAuth: "telemost",
	}
	out, err := p.Autofill(inbound)
	if err != nil {
		t.Fatalf("Autofill error: %v", err)
	}
	if out.OlcrtcRoomID != "" {
		t.Errorf("expected no auto room for telemost, got %q", out.OlcrtcRoomID)
	}
	if out.ProtocolFields["olcrtcRoomID"] != "" {
		t.Errorf("expected empty protocolFields room for telemost, got %v", out.ProtocolFields["olcrtcRoomID"])
	}
}

func TestIsOlcrtcKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{strings.Repeat("a", 64), true},
		{strings.Repeat("0", 64), true},
		{"abc", false},
		{strings.Repeat("A", 64), false},
		{strings.Repeat("a", 63) + "g", false},
	}
	for _, tc := range cases {
		if got := isOlcrtcKey(tc.key); got != tc.want {
			t.Errorf("isOlcrtcKey(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}
