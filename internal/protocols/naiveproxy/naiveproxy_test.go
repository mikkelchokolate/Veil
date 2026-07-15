package naiveproxy

import (
	"path/filepath"
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

	if got := p.Protocol(); got != "naiveproxy" {
		t.Errorf("Protocol() = %q, want naiveproxy", got)
	}
	if got := p.DisplayName(); got != "NaiveProxy" {
		t.Errorf("DisplayName() = %q, want NaiveProxy", got)
	}
	if got := p.Transports(); !reflect.DeepEqual(got, []string{"tcp"}) {
		t.Errorf("Transports() = %v, want [tcp]", got)
	}
	if got := p.RequiresCaddy(); got != true {
		t.Errorf("RequiresCaddy() = %v, want true", got)
	}
	if got := p.FirewallService(); got != "Veil NaiveProxy" {
		t.Errorf("FirewallService() = %q, want Veil NaiveProxy", got)
	}
	if got := p.MaxEnabled(); got != 0 {
		t.Errorf("MaxEnabled() = %v, want 0", got)
	}
}

func TestRenderConfigWithInbound(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain: "example.com",
		Email:  "admin@example.com",
	}
	inbound := model.Inbound{
		Name:      "naive1",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		ProtocolFields: map[string]any{
			"naiveUsername": "user1",
			"naivePassword": "pass1",
			"fallbackRoot":  "/var/lib/veil/www",
		},
	}
	input := generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths("/tmp/veil"),
		Inbounds: []model.Inbound{inbound},
	}

	artifacts, ok, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig error: %v", err)
	}
	if !ok {
		t.Fatal("RenderConfig ok = false, want true")
	}
	if len(artifacts) != 1 {
		t.Fatalf("len(artifacts) = %d, want 1", len(artifacts))
	}

	wantPath := filepath.Join("/tmp/veil", "generated", "caddy", "naive1.Caddyfile")
	if artifacts[0].Path != wantPath {
		t.Errorf("artifact path = %q, want %q", artifacts[0].Path, wantPath)
	}
	body := artifacts[0].Body
	for _, want := range []string{"example.com", "user1", "pass1", "forward_proxy", "file_server"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

func TestRenderConfigNoInboundsPanelAccessCaddy(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:      "example.com",
		Email:       "admin@example.com",
		PanelAccess: "caddy",
		PanelListen: "127.0.0.1:8080",
		WebBasePath: "/panel",
	}
	input := generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths("/tmp/veil"),
		Inbounds: []model.Inbound{},
	}

	artifacts, ok, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig error: %v", err)
	}
	if !ok {
		t.Fatal("RenderConfig ok = false, want true")
	}
	if len(artifacts) != 1 {
		t.Fatalf("len(artifacts) = %d, want 1", len(artifacts))
	}

	wantPath := filepath.Join("/tmp/veil", "generated", "caddy", "panel.Caddyfile")
	if artifacts[0].Path != wantPath {
		t.Errorf("artifact path = %q, want %q", artifacts[0].Path, wantPath)
	}
	body := artifacts[0].Body
	for _, want := range []string{"example.com", "reverse_proxy", "127.0.0.1:8080", "/panel"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

func TestRenderConfigNoInboundsInvalidPanelListen(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:      "example.com",
		Email:       "admin@example.com",
		PanelAccess: "caddy",
		PanelListen: "not-a-host-port",
		WebBasePath: "/panel",
	}
	input := generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths("/tmp/veil"),
		Inbounds: []model.Inbound{},
	}

	_, _, err := p.RenderConfig(input)
	if err == nil {
		t.Fatal("expected error for invalid panelListen, got nil")
	}
	if !strings.Contains(err.Error(), "panelListen must be host:port") {
		t.Errorf("error = %q, want panelListen host:port error", err.Error())
	}
}

func TestRenderConfigNoInboundsInvalidPanelPort(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:      "example.com",
		Email:       "admin@example.com",
		PanelAccess: "caddy",
		PanelListen: "127.0.0.1:abc",
		WebBasePath: "/panel",
	}
	input := generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths("/tmp/veil"),
		Inbounds: []model.Inbound{},
	}

	_, _, err := p.RenderConfig(input)
	if err == nil {
		t.Fatal("expected error for invalid panel port, got nil")
	}
	if !strings.Contains(err.Error(), "panelListen must be host:port") {
		t.Errorf("error = %q, want panelListen host:port error", err.Error())
	}
}

func TestRenderConfigNoInboundsEmptyDomain(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:      "",
		Email:       "admin@example.com",
		PanelAccess: "caddy",
		PanelListen: "127.0.0.1:8080",
		WebBasePath: "/panel",
	}
	input := generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths("/tmp/veil"),
		Inbounds: []model.Inbound{},
	}

	_, _, err := p.RenderConfig(input)
	if err == nil {
		t.Fatal("expected error for empty domain, got nil")
	}
}

func TestRenderConfigPanelAccessMissingWebBasePath(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:      "example.com",
		Email:       "admin@example.com",
		PanelAccess: "caddy",
		PanelListen: "127.0.0.1:8080",
		WebBasePath: "/",
	}
	input := generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths("/tmp/veil"),
		Inbounds: []model.Inbound{},
	}

	_, _, err := p.RenderConfig(input)
	if err == nil {
		t.Fatal("expected error for missing webBasePath, got nil")
	}
	if !strings.Contains(err.Error(), "webBasePath is required") {
		t.Errorf("error = %q, want webBasePath required error", err.Error())
	}
}

func TestRenderConfigNoInboundsNoPanelAccess(t *testing.T) {
	p := New()
	settings := model.Settings{Domain: "example.com"}
	input := generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths("/tmp/veil"),
		Inbounds: []model.Inbound{},
	}

	artifacts, ok, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig error: %v", err)
	}
	if ok {
		t.Fatal("RenderConfig ok = true, want false")
	}
	if len(artifacts) != 0 {
		t.Fatalf("len(artifacts) = %d, want 0", len(artifacts))
	}
}

func TestRenderConfigWithWarp(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain: "example.com",
		Email:  "admin@example.com",
	}
	inbound := model.Inbound{
		Name:      "naive-warp",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		ProtocolFields: map[string]any{
			"naiveUsername": "u",
			"naivePassword": "p",
		},
	}
	warp := model.WarpConfig{Enabled: true, SocksPort: 40001}
	input := generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths("/tmp/veil"),
		Inbounds: []model.Inbound{inbound},
		Warp:     warp,
	}

	artifacts, ok, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig error: %v", err)
	}
	if !ok || len(artifacts) != 1 {
		t.Fatalf("RenderConfig ok=%v len=%d, want true/1", ok, len(artifacts))
	}
	if !strings.Contains(artifacts[0].Body, "socks5://127.0.0.1:40001") {
		t.Errorf("body missing warp upstream:\n%s", artifacts[0].Body)
	}
}

func TestRenderConfigProtocolFieldsNonStringFallsBack(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:        "example.com",
		Email:         "admin@example.com",
		NaiveUsername: "globaluser",
		NaivePassword: "globalpass",
	}
	inbound := model.Inbound{
		Name:          "naive-nonstring",
		Protocol:      "naiveproxy",
		Transport:     "tcp",
		Port:          8443,
		NaiveUsername: "legacyuser",
		NaivePassword: "legacypass",
		ProtocolFields: map[string]any{
			"naiveUsername": 123,
			"naivePassword": true,
		},
	}
	input := generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths("/tmp/veil"),
		Inbounds: []model.Inbound{inbound},
	}

	artifacts, _, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig error: %v", err)
	}
	body := artifacts[0].Body
	if !strings.Contains(body, "legacyuser") || !strings.Contains(body, "legacypass") {
		t.Errorf("body missing legacy credentials after non-string protocolFields:\n%s", body)
	}
}

func TestRenderConfigFallbackRootAndLegacyCredentials(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:        "example.com",
		Email:         "admin@example.com",
		NaiveUsername: "globaluser",
		NaivePassword: "globalpass",
		FallbackRoot:  "/var/lib/veil/global",
	}
	inbound := model.Inbound{
		Name:          "naive-legacy",
		Protocol:      "naiveproxy",
		Transport:     "tcp",
		Port:          8443,
		NaiveUsername: "legacyuser",
		NaivePassword: "legacypass",
		FallbackRoot:  "/var/lib/veil/inbound",
	}

	input := generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths("/tmp/veil"),
		Inbounds: []model.Inbound{inbound},
	}

	artifacts, _, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig error: %v", err)
	}
	body := artifacts[0].Body
	if !strings.Contains(body, "legacyuser") {
		t.Errorf("body missing legacy username:\n%s", body)
	}
	if !strings.Contains(body, "legacypass") {
		t.Errorf("body missing legacy password:\n%s", body)
	}
	if !strings.Contains(body, "/var/lib/veil/inbound") {
		t.Errorf("body missing inbound fallback root:\n%s", body)
	}
}

func TestRenderConfigProtocolFieldsOverrideLegacy(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:        "example.com",
		Email:         "admin@example.com",
		NaiveUsername: "globaluser",
		NaivePassword: "globalpass",
	}
	inbound := model.Inbound{
		Name:          "naive-pf",
		Protocol:      "naiveproxy",
		Transport:     "tcp",
		Port:          8443,
		NaiveUsername: "legacyuser",
		NaivePassword: "legacypass",
		ProtocolFields: map[string]any{
			"naiveUsername": "pfuser",
			"naivePassword": "pfpass",
			"fallbackRoot":  "/var/lib/veil/pf",
		},
	}

	input := generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths("/tmp/veil"),
		Inbounds: []model.Inbound{inbound},
	}

	artifacts, _, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig error: %v", err)
	}
	body := artifacts[0].Body
	if !strings.Contains(body, "pfuser") {
		t.Errorf("body missing protocolFields username:\n%s", body)
	}
	if !strings.Contains(body, "pfpass") {
		t.Errorf("body missing protocolFields password:\n%s", body)
	}
	if !strings.Contains(body, "/var/lib/veil/pf") {
		t.Errorf("body missing protocolFields fallback root:\n%s", body)
	}
	if strings.Contains(body, "legacyuser") || strings.Contains(body, "legacypass") {
		t.Errorf("body unexpectedly contains legacy credentials:\n%s", body)
	}
}

func TestRenderConfigIncludesPanelOnFirstInboundWhenNo443(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:      "example.com",
		Email:       "admin@example.com",
		PanelAccess: "caddy",
		PanelListen: "127.0.0.1:8080",
		WebBasePath: "/panel",
	}
	inbound := model.Inbound{
		Name:      "naive-8443",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      8443,
		ProtocolFields: map[string]any{
			"naiveUsername": "u",
			"naivePassword": "p",
		},
	}
	input := generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths("/tmp/veil"),
		Inbounds: []model.Inbound{inbound},
	}

	artifacts, _, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig error: %v", err)
	}
	if !strings.Contains(artifacts[0].Body, "handle /panel") {
		t.Errorf("body missing panel route:\n%s", artifacts[0].Body)
	}
}

func TestRenderConfigErrorInvalidPort(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain: "example.com",
		Email:  "admin@example.com",
	}
	inbound := model.Inbound{
		Name:      "naive-bad",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      0,
		ProtocolFields: map[string]any{
			"naiveUsername": "u",
			"naivePassword": "p",
		},
	}
	input := generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths("/tmp/veil"),
		Inbounds: []model.Inbound{inbound},
	}

	_, _, err := p.RenderConfig(input)
	if err == nil {
		t.Fatal("expected error for invalid port, got nil")
	}
}

func TestRenderConfigInvalidPanelListenErrors(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:      "example.com",
		Email:       "admin@example.com",
		PanelAccess: "caddy",
		PanelListen: "not-a-valid-address",
		WebBasePath: "/panel",
	}
	inbound := model.Inbound{
		Name:      "naive-panel-err",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		ProtocolFields: map[string]any{
			"naiveUsername": "u",
			"naivePassword": "p",
		},
	}
	input := generatedconfig.ProtocolRenderInput{
		Settings: settings,
		Paths:    generatedconfig.NewPaths("/tmp/veil"),
		Inbounds: []model.Inbound{inbound},
	}

	_, _, err := p.RenderConfig(input)
	if err == nil {
		t.Fatalf("expected error for invalid panelListen with caddy panel access, got nil")
	}
	if !strings.Contains(err.Error(), "panelListen must be host:port") {
		t.Fatalf("expected panelListen error, got: %v", err)
	}
}

func TestArtifactSpec(t *testing.T) {
	p := New()
	spec := p.ArtifactSpec()

	if spec.Subpath != generatedconfig.CaddyfileSubpath {
		t.Errorf("Subpath = %q, want %q", spec.Subpath, generatedconfig.CaddyfileSubpath)
	}
	if spec.ValidationName != "caddy" {
		t.Errorf("ValidationName = %q, want caddy", spec.ValidationName)
	}
	want := []string{"caddy", "validate", "--config", "/tmp/Caddyfile"}
	if got := spec.ValidationCommand("/tmp/Caddyfile"); !reflect.DeepEqual(got, want) {
		t.Errorf("ValidationCommand = %v, want %v", got, want)
	}
}

func TestRuntimeDescriptorsWithMatchingInbounds(t *testing.T) {
	p := New()
	inbounds := []model.Inbound{
		{Name: "n1", Protocol: "naiveproxy", Enabled: true, Transport: "tcp", Port: 443},
		{Name: "n2", Protocol: "naiveproxy", Enabled: true, Transport: "tcp", Port: 8443},
		{Name: "h1", Protocol: "hysteria2", Enabled: true, Transport: "udp", Port: 443},
	}

	runtimes := p.RuntimeDescriptors(inbounds)
	if len(runtimes) != 1 {
		t.Fatalf("len(runtimes) = %d, want one consolidated Caddy runtime", len(runtimes))
	}
	want := service.ManagedRuntime{
		Name:             caddyUnit,
		ActionName:       "caddy",
		Protocol:         "naiveproxy",
		Transport:        "tcp",
		Unit:             caddyUnit,
		PromotedSubpath:  generatedconfig.CaddyJSONConfigSubpath,
		PromotedVerb:     "reload",
		ManualRestart:    true,
		HealthCheckAfter: true,
	}
	if !reflect.DeepEqual(runtimes[0], want) {
		t.Errorf("runtime = %+v, want %+v", runtimes[0], want)
	}
}

func TestRuntimeDescriptorsNoMatchingInbounds(t *testing.T) {
	p := New()
	runtimes := p.RuntimeDescriptors([]model.Inbound{
		{Name: "h1", Protocol: "hysteria2", Enabled: true, Transport: "udp", Port: 443},
	})
	if len(runtimes) != 0 {
		t.Fatalf("len(runtimes) = %d, want no naiveproxy runtime", len(runtimes))
	}
}

func TestRuntimeInstall(t *testing.T) {
	p := New()
	rt := p.RuntimeInstall("naiveproxy")
	if rt.Name != "naiveproxy" {
		t.Errorf("Name = %q, want naiveproxy", rt.Name)
	}
	if rt.Binary != "caddy" {
		t.Errorf("Binary = %q, want caddy", rt.Binary)
	}
	if rt.Method != runtimeinstall.MethodCaddyNaive {
		t.Errorf("Method = %q, want %q", rt.Method, runtimeinstall.MethodCaddyNaive)
	}
}

func TestValidateSettings(t *testing.T) {
	p := New()
	valid := model.Settings{
		Domain: "example.com",
		Email:  "admin@example.com",
		ProtocolFields: map[string]any{
			"naiveUsername": "u",
			"naivePassword": "p",
		},
	}
	if err := p.ValidateSettings(valid, model.Inbound{}); err != nil {
		t.Errorf("ValidateSettings(valid) = %v, want nil", err)
	}

	if err := p.ValidateSettings(valid, model.Inbound{NaiveUsername: "u", NaivePassword: "p"}); err != nil {
		t.Errorf("ValidateSettings with inbound credentials = %v, want nil", err)
	}

	cases := []struct {
		name string
		mod  func(*model.Settings)
	}{
		{"missing domain", func(s *model.Settings) { s.Domain = "" }},
		{"missing email", func(s *model.Settings) { s.Email = "" }},
		{"missing username", func(s *model.Settings) { s.ProtocolFields = map[string]any{"naivePassword": "p"} }},
		{"missing password", func(s *model.Settings) { s.ProtocolFields = map[string]any{"naiveUsername": "u"} }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := valid
			tc.mod(&s)
			err := p.ValidateSettings(s, model.Inbound{})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "naive") {
				t.Errorf("error message = %q, want required-fields message", err.Error())
			}
		})
	}
}

func TestValidateInbound(t *testing.T) {
	p := New()
	settings := model.Settings{}

	issues := p.ValidateInbound(settings, model.Inbound{})
	if len(issues) != 1 {
		t.Fatalf("ValidateInbound empty = %v, want 1 issue", issues)
	}
	if issues[0].Code != "naive_credential_required" {
		t.Errorf("issue code = %q, want naive_credential_required", issues[0].Code)
	}

	noIssues := p.ValidateInbound(settings, model.Inbound{NaiveUsername: "u", NaivePassword: "p"})
	if len(noIssues) != 0 {
		t.Errorf("ValidateInbound with credentials = %v, want empty", noIssues)
	}

	fallbackIssues := p.ValidateInbound(settings, model.Inbound{})
	if len(fallbackIssues) != 1 {
		t.Errorf("ValidateInbound without fallback = %v, want 1 issue", fallbackIssues)
	}

	fallbackOk := p.ValidateInbound(model.Settings{NaiveUsername: "u", NaivePassword: "p"}, model.Inbound{})
	if len(fallbackOk) != 0 {
		t.Errorf("ValidateInbound with fallback settings = %v, want empty", fallbackOk)
	}
}

func TestNeedsDomain(t *testing.T) {
	p := New()
	if !p.NeedsDomain(model.Settings{}, model.Inbound{}) {
		t.Error("NeedsDomain = false, want true")
	}
}

func TestHasCredential(t *testing.T) {
	p := New()
	settings := model.Settings{
		ProtocolFields: map[string]any{
			"naiveUsername": "fallbackuser",
			"naivePassword": "fallbackpass",
		},
	}

	t.Run("enabled profile", func(t *testing.T) {
		inbound := model.Inbound{Profiles: []model.ClientProfile{
			{Name: "pro1", Username: "u", Password: "p", Enabled: true},
		}}
		if !p.HasCredential(settings, inbound) {
			t.Error("HasCredential = false, want true for enabled profile")
		}
	})

	t.Run("disabled profile falls back", func(t *testing.T) {
		inbound := model.Inbound{Profiles: []model.ClientProfile{
			{Name: "pro1", Username: "u", Password: "p", Enabled: false},
		}}
		if !p.HasCredential(settings, inbound) {
			t.Error("HasCredential = false, want true with fallback credentials")
		}
	})

	t.Run("profile without password", func(t *testing.T) {
		inbound := model.Inbound{Profiles: []model.ClientProfile{
			{Name: "pro1", Username: "u", Password: "", Enabled: true},
		}}
		emptySettings := model.Settings{}
		if p.HasCredential(emptySettings, inbound) {
			t.Error("HasCredential = true, want false for profile without password and no fallback")
		}
	})

	t.Run("fallback credentials", func(t *testing.T) {
		inbound := model.Inbound{}
		if !p.HasCredential(settings, inbound) {
			t.Error("HasCredential = false, want true with fallback credentials")
		}
	})

	t.Run("no credentials", func(t *testing.T) {
		inbound := model.Inbound{}
		emptySettings := model.Settings{}
		if p.HasCredential(emptySettings, inbound) {
			t.Error("HasCredential = true, want false with no credentials")
		}
	})
}

func TestInboundFieldSchema(t *testing.T) {
	p := New()
	fields := p.InboundFieldSchema()
	if len(fields) != 3 {
		t.Fatalf("len(fields) = %d, want 3", len(fields))
	}
	want := []schema.FieldSchema{
		{Key: "naiveUsername", Label: "Naive Username", Type: schema.FieldText, Default: "veil", Scope: "inbound"},
		{Key: "naivePassword", Label: "Naive Password", Type: schema.FieldPassword, GenerateAction: "password", Scope: "inbound"},
		{Key: "fallbackRoot", Label: "Fallback Root", Type: schema.FieldText, Default: "/var/lib/veil/www", Scope: "inbound"},
	}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("InboundFieldSchema = %+v, want %+v", fields, want)
	}
}

func TestSettingsFieldSchema(t *testing.T) {
	p := New()
	fields := p.SettingsFieldSchema()
	if len(fields) != 3 {
		t.Fatalf("len(fields) = %d, want 3", len(fields))
	}
	want := []schema.FieldSchema{
		{Key: "naiveUsername", Label: "Naive Username", Type: schema.FieldText, Default: "veil", Scope: "settings"},
		{Key: "naivePassword", Label: "Naive Password", Type: schema.FieldPassword, Scope: "settings"},
		{Key: "fallbackRoot", Label: "Fallback Root", Type: schema.FieldText, Default: "/var/lib/veil/www", Scope: "settings"},
	}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("SettingsFieldSchema = %+v, want %+v", fields, want)
	}
}

func TestAutofill(t *testing.T) {
	p := New()
	inbound := model.Inbound{Name: "n1"}
	out, err := p.Autofill(inbound)
	if err != nil {
		t.Fatalf("Autofill error: %v", err)
	}
	if !reflect.DeepEqual(out, inbound) {
		t.Errorf("Autofill = %+v, want %+v", out, inbound)
	}
}

func TestBuildLinksNoDomain(t *testing.T) {
	p := New()
	settings := model.Settings{}
	inbound := model.Inbound{Name: "n1", Protocol: "naiveproxy", Transport: "tcp", Port: 443}
	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("BuildLinks error: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("len(links) = %d, want 0", len(links))
	}
}

func TestBuildLinksFallbackFromLegacySettingsOnly(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:        "example.com",
		NaiveUsername: "settingsuser",
		NaivePassword: "settingspass",
	}
	inbound := model.Inbound{
		Name:      "n1",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
	}

	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("BuildLinks error: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("len(links) = %d, want 1", len(links))
	}
	want := clientaccess.NaiveClientURI("example.com", 443, "settingsuser", "settingspass")
	if links[0].URI != want {
		t.Errorf("link.URI = %q, want %q", links[0].URI, want)
	}
}

func TestBuildLinksFallbackCredentials(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:        "example.com",
		NaiveUsername: "fallbackuser",
		NaivePassword: "fallbackpass",
	}
	inbound := model.Inbound{
		Name:      "n1",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
	}

	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("BuildLinks error: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("len(links) = %d, want 1", len(links))
	}
	if links[0].Name != "n1" {
		t.Errorf("link.Name = %q, want n1", links[0].Name)
	}
	want := clientaccess.NaiveClientURI("example.com", 443, "fallbackuser", "fallbackpass")
	if links[0].URI != want {
		t.Errorf("link.URI = %q, want %q", links[0].URI, want)
	}
}

func TestBuildLinksWithProfiles(t *testing.T) {
	p := New()
	settings := model.Settings{Domain: "example.com"}
	inbound := model.Inbound{
		Name:      "n1",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		Profiles: []model.ClientProfile{
			{Name: "pro1", Username: "u1", Password: "p1", Enabled: true},
			{Name: "pro2", Username: "u2", Password: "p2", Enabled: false},
		},
	}

	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("BuildLinks error: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("len(links) = %d, want 1", len(links))
	}
	if links[0].Name != "n1/pro1" {
		t.Errorf("link.Name = %q, want n1/pro1", links[0].Name)
	}
	want := clientaccess.NaiveClientURI("example.com", 443, "u1", "p1")
	if links[0].URI != want {
		t.Errorf("link.URI = %q, want %q", links[0].URI, want)
	}
}

func TestBuildLinksInvalidProfiles(t *testing.T) {
	p := New()
	settings := model.Settings{Domain: "example.com"}
	inbound := model.Inbound{
		Name:      "n1",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		Profiles: []model.ClientProfile{
			{Name: "pro1", Username: "u1", Password: "", Enabled: true},
		},
	}

	_, err := p.BuildLinks(settings, inbound)
	if err == nil {
		t.Fatal("expected error for invalid profile, got nil")
	}
}

func TestBuildLinksMultipleProfiles(t *testing.T) {
	p := New()
	settings := model.Settings{Domain: "example.com"}
	inbound := model.Inbound{
		Name:      "n1",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		Profiles: []model.ClientProfile{
			{Name: "pro1", Username: "u1", Password: "p1", Enabled: true},
			{Name: "pro2", Username: "u2", Password: "p2", Enabled: true},
		},
	}

	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("BuildLinks error: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("len(links) = %d, want 2", len(links))
	}
	for i, wantName := range []string{"n1/pro1", "n1/pro2"} {
		if links[i].Name != wantName {
			t.Errorf("links[%d].Name = %q, want %q", i, links[i].Name, wantName)
		}
	}
}
