package naiveproxy

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
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
		Enabled:   true,
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

	wantPath := filepath.Join("/tmp/veil", "generated", "caddy", "config.json")
	if artifacts[0].Path != wantPath {
		t.Errorf("artifact path = %q, want %q", artifacts[0].Path, wantPath)
	}
	body := artifacts[0].Body
	for _, want := range []string{"example.com", "forward_proxy", "file_server"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
}

func TestRenderConfigNoInboundsReturnsNoArtifacts(t *testing.T) {
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
	if ok {
		t.Fatal("RenderConfig ok = true, want false")
	}
	if len(artifacts) != 0 {
		t.Fatalf("len(artifacts) = %d, want 0", len(artifacts))
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
		Enabled:   true,
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
	// WARP upstream is applied to the forward_proxy handler in Task 15; for now
	// just verify the consolidated JSON config is emitted.
	if artifacts[0].Path != filepath.Join("/tmp/veil", "generated", "caddy", "config.json") {
		t.Errorf("unexpected path %q", artifacts[0].Path)
	}
}

func TestRenderConfigInboundProtocolFieldsOverride(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:                   "settings.example.com",
		Email:                    "admin@example.com",
		DefaultInboundPublicPort: 8443,
	}
	inbound := model.Inbound{
		Name:      "naive-pf",
		Protocol:  "naiveproxy",
		Enabled:   true,
		Transport: "tcp",
		Port:      8443,
		Profiles: []model.ClientProfile{
			{Name: "pro1", Username: "pfuser", Password: "pfpass", Enabled: true},
		},
		ProtocolFields: map[string]any{
			"domain":       "pf.example.com",
			"publicPort":   9443,
			"fallbackRoot": "/var/lib/veil/pf",
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
	for _, want := range []string{"pf.example.com", "forward_proxy", "file_server"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `"root": "/var/lib/veil/pf"`) {
		t.Errorf("expected inbound fallback root in file_server, got:\n%s", body)
	}
	if strings.Contains(body, "settings.example.com") {
		t.Errorf("body unexpectedly contains settings-level domain:\n%s", body)
	}
	if !strings.Contains(body, "9443") {
		t.Errorf("body missing protocolFields publicPort 9443:\n%s", body)
	}
}

func TestRenderConfigIncludesPanelServerWhenPanelAccessCaddy(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:          "example.com",
		Email:           "admin@example.com",
		PanelAccess:     "caddy",
		PanelDomain:     "panel.example.com",
		PanelEmail:      "admin@example.com",
		PanelPublicPort: 443,
		PanelListen:     "127.0.0.1:8080",
		WebBasePath:     "/panel",
	}
	inbound := model.Inbound{
		Name:      "naive-8443",
		Protocol:  "naiveproxy",
		Enabled:   true,
		Transport: "tcp",
		Port:      8443,
		Profiles: []model.ClientProfile{
			{Name: "pro1", Username: "u", Password: "p", Enabled: true},
		},
		ProtocolFields: map[string]any{
			"domain": "proxy.example.com",
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
	if !strings.Contains(body, "panel.example.com") {
		t.Errorf("body missing panel domain host matcher:\n%s", body)
	}
	if !strings.Contains(body, `"handler": "reverse_proxy"`) {
		t.Errorf("body missing reverse_proxy handler for panel server:\n%s", body)
	}
	if !strings.Contains(body, "127.0.0.1:8080") {
		t.Errorf("body missing panel reverse_proxy dial:\n%s", body)
	}
}

func TestRenderConfigDefaultsPublicPort(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:                   "example.com",
		Email:                    "admin@example.com",
		DefaultInboundPublicPort: 8443,
	}
	inbound := model.Inbound{
		Name:      "naive-default-port",
		Protocol:  "naiveproxy",
		Enabled:   true,
		Transport: "tcp",
		Port:      0,
		ProtocolFields: map[string]any{
			"domain":        "example.com",
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
	if !strings.Contains(artifacts[0].Body, "8443") {
		t.Errorf("expected default publicPort 8443 in JSON, got:\n%s", artifacts[0].Body)
	}
}

func TestRenderConfigInvalidPanelListenIgnored(t *testing.T) {
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
		Enabled:   true,
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

	artifacts, _, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig error: %v", err)
	}
	// panelCaddyRoute errors are intentionally swallowed by renderNaive.
	if strings.Contains(artifacts[0].Body, "handle /panel") {
		t.Errorf("body unexpectedly contains panel route with invalid panelListen:\n%s", artifacts[0].Body)
	}
}

func TestArtifactSpec(t *testing.T) {
	p := New()
	spec := p.ArtifactSpec()

	if spec.Subpath != generatedconfig.CaddyJSONConfigSubpath {
		t.Errorf("Subpath = %q, want %q", spec.Subpath, generatedconfig.CaddyJSONConfigSubpath)
	}
	if spec.ValidationName != "caddy" {
		t.Errorf("ValidationName = %q, want caddy", spec.ValidationName)
	}
	want := []string{"caddy", "validate", "--config", "/tmp/config.json", "--adapter", "json"}
	if got := spec.ValidationCommand("/tmp/config.json"); !reflect.DeepEqual(got, want) {
		t.Errorf("ValidationCommand = %v, want %v", got, want)
	}
}

func TestRuntimeDescriptorsSingleCaddyService(t *testing.T) {
	p := New()
	inbounds := []model.Inbound{
		{Name: "naive-1", Protocol: "naiveproxy"},
		{Name: "naive-2", Protocol: "naiveproxy"},
	}
	runtimes := p.RuntimeDescriptors(inbounds)
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 Caddy runtime, got %d", len(runtimes))
	}
	if runtimes[0].Name != "veil-caddy.service" {
		t.Errorf("runtime name = %q", runtimes[0].Name)
	}
}

func TestRuntimeDescriptorsWithMatchingInbounds(t *testing.T) {
	p := New()
	inbounds := []model.Inbound{
		{Name: "n1", Protocol: "naiveproxy", Transport: "tcp", Port: 443},
		{Name: "n2", Protocol: "naiveproxy", Transport: "tcp", Port: 8443},
		{Name: "h1", Protocol: "hysteria2", Transport: "udp", Port: 443},
	}

	runtimes := p.RuntimeDescriptors(inbounds)
	if len(runtimes) != 1 {
		t.Fatalf("len(runtimes) = %d, want 1", len(runtimes))
	}
	if runtimes[0].Name != "veil-caddy.service" {
		t.Errorf("runtime name = %q, want veil-caddy.service", runtimes[0].Name)
	}
}

func TestRuntimeDescriptorsNoMatchingInbounds(t *testing.T) {
	p := New()
	runtimes := p.RuntimeDescriptors([]model.Inbound{
		{Name: "h1", Protocol: "hysteria2", Transport: "udp", Port: 443},
	})
	if len(runtimes) != 0 {
		t.Fatalf("len(runtimes) = %d, want 0", len(runtimes))
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
	if err := p.ValidateSettings(valid); err != nil {
		t.Errorf("ValidateSettings(valid) = %v, want nil", err)
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
			err := p.ValidateSettings(s)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "domain, email, naive username, and naive password") {
				t.Errorf("error message = %q, want required-fields message", err.Error())
			}
		})
	}
}

func TestValidateInbound(t *testing.T) {
	p := New()
	valid := model.Inbound{
		Protocol: "naiveproxy",
		ProtocolFields: map[string]any{
			"domain":        "x.com",
			"naiveUsername": "u",
			"naivePassword": "p",
		},
	}
	issues := p.ValidateInbound(model.Settings{}, valid)
	if len(issues) != 0 {
		t.Errorf("ValidateInbound = %v, want empty", issues)
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

func TestNaiveProtocolFieldHelpers(t *testing.T) {
	settings := model.Settings{DefaultInboundPublicPort: 8443, FallbackRoot: "/var/lib/veil/www"}
	inbound := model.Inbound{
		Protocol: "naiveproxy",
		ProtocolFields: map[string]any{
			"domain":     "p.example.com",
			"email":      "a@example.com",
			"publicPort": 9443,
			"transport":  "dual",
		},
	}
	if got := NaiveDomain(settings, inbound); got != "p.example.com" {
		t.Errorf("domain = %q", got)
	}
	if got := NaivePublicPort(settings, inbound); got != 9443 {
		t.Errorf("publicPort = %d", got)
	}
	if got := NaiveTransport(inbound); got != "dual" {
		t.Errorf("transport = %q", got)
	}
}

func TestInboundFieldSchema(t *testing.T) {
	p := New()
	fields := p.InboundFieldSchema()
	if len(fields) != 7 {
		t.Fatalf("len(fields) = %d, want 7", len(fields))
	}
	want := []schema.FieldSchema{
		{Key: "domain", Label: "Domain", Type: schema.FieldText, Required: true, Placeholder: "Public domain used for TLS/SNI and client export.", Scope: "inbound"},
		{Key: "email", Label: "ACME email", Type: schema.FieldText, Placeholder: "Optional explicit ACME contact for this domain.", Scope: "inbound"},
		{Key: "publicPort", Label: "Public port", Type: schema.FieldNumber, Default: 443, Placeholder: "Port Caddy listens on for this inbound.", Scope: "inbound"},
		{Key: "transport", Label: "Transport", Type: schema.FieldSelect, Required: true, Default: "tcp", Options: []schema.FieldOption{{Label: "tcp", Value: "tcp"}, {Label: "quic", Value: "quic"}, {Label: "dual", Value: "dual"}}, Placeholder: "tcp=HTTPS/H2, quic=HTTP/3/QUIC, dual=both.", Scope: "inbound"},
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
	if len(fields) != 7 {
		t.Fatalf("len(fields) = %d, want 7", len(fields))
	}
	want := []schema.FieldSchema{
		{Key: "naiveUsername", Label: "Naive Username", Type: schema.FieldText, Default: "veil", Scope: "settings"},
		{Key: "naivePassword", Label: "Naive Password", Type: schema.FieldPassword, Scope: "settings"},
		{Key: "fallbackRoot", Label: "Fallback Root", Type: schema.FieldText, Default: "/var/lib/veil/www", Scope: "settings"},
		{Key: "panelAccess", Label: "Panel Access", Type: schema.FieldSelect, Default: "local", Options: []schema.FieldOption{{Label: "local", Value: "local"}, {Label: "direct", Value: "direct"}, {Label: "caddy", Value: "caddy"}}, Scope: "settings"},
		{Key: "panelDomain", Label: "Panel Domain", Type: schema.FieldText, Scope: "settings", Placeholder: "Public domain used for Panel Caddy TLS/SNI."},
		{Key: "panelEmail", Label: "Panel ACME Email", Type: schema.FieldText, Scope: "settings", Placeholder: "ACME contact email for Panel Caddy certificate."},
		{Key: "panelPublicPort", Label: "Panel Public Port", Type: schema.FieldNumber, Default: 443, Scope: "settings", Placeholder: "Port Caddy listens on for Panel access."},
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

func TestBuildLinksTCP(t *testing.T) {
	p := New()
	settings := model.Settings{DefaultInboundPublicPort: 443}
	inbound := model.Inbound{
		Name:     "n1",
		Protocol: "naiveproxy",
		Enabled:  true,
		Profiles: []model.ClientProfile{{Name: "pro1", Username: "u1", Password: "p1", Enabled: true}},
		ProtocolFields: map[string]any{
			"domain":    "example.com",
			"transport": "tcp",
		},
	}

	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("BuildLinks error: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("len(links) = %d, want 1", len(links))
	}
	if links[0].Name != "n1-https" {
		t.Errorf("link.Name = %q, want n1-https", links[0].Name)
	}
	if links[0].Transport != "tcp" {
		t.Errorf("link.Transport = %q, want tcp", links[0].Transport)
	}
	want := "https://u1:p1@example.com"
	if links[0].URI != want {
		t.Errorf("link.URI = %q, want %q", links[0].URI, want)
	}
}

func TestBuildLinksQUIC(t *testing.T) {
	p := New()
	settings := model.Settings{DefaultInboundPublicPort: 443}
	inbound := model.Inbound{
		Name:     "n1",
		Protocol: "naiveproxy",
		Enabled:  true,
		Profiles: []model.ClientProfile{{Name: "pro1", Username: "u1", Password: "p1", Enabled: true}},
		ProtocolFields: map[string]any{
			"domain":    "example.com",
			"transport": "quic",
		},
	}

	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("BuildLinks error: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("len(links) = %d, want 1", len(links))
	}
	if links[0].Name != "n1-quic" {
		t.Errorf("link.Name = %q, want n1-quic", links[0].Name)
	}
	if links[0].Transport != "quic" {
		t.Errorf("link.Transport = %q, want quic", links[0].Transport)
	}
	want := "quic://u1:p1@example.com"
	if links[0].URI != want {
		t.Errorf("link.URI = %q, want %q", links[0].URI, want)
	}
}

func TestBuildLinksNonDefaultPort(t *testing.T) {
	p := New()
	settings := model.Settings{DefaultInboundPublicPort: 8443}
	inbound := model.Inbound{
		Name:     "n1",
		Protocol: "naiveproxy",
		Enabled:  true,
		Profiles: []model.ClientProfile{{Name: "pro1", Username: "u1", Password: "p1", Enabled: true}},
		ProtocolFields: map[string]any{
			"domain":    "example.com",
			"transport": "tcp",
		},
	}

	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("BuildLinks error: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("len(links) = %d, want 1", len(links))
	}
	want := "https://u1:p1@example.com:8443"
	if links[0].URI != want {
		t.Errorf("link.URI = %q, want %q", links[0].URI, want)
	}
}

func TestBuildLinksNoProfiles(t *testing.T) {
	p := New()
	settings := model.Settings{DefaultInboundPublicPort: 443}
	inbound := model.Inbound{
		Name:     "n1",
		Protocol: "naiveproxy",
		Enabled:  true,
		ProtocolFields: map[string]any{
			"domain":    "example.com",
			"transport": "tcp",
		},
	}

	_, err := p.BuildLinks(settings, inbound)
	if err == nil {
		t.Fatal("expected error for missing profiles, got nil")
	}
}

func TestBuildLinksMultipleProfiles(t *testing.T) {
	p := New()
	settings := model.Settings{DefaultInboundPublicPort: 443}
	inbound := model.Inbound{
		Name:     "n1",
		Protocol: "naiveproxy",
		Enabled:  true,
		Profiles: []model.ClientProfile{
			{Name: "pro1", Username: "u1", Password: "p1", Enabled: true},
			{Name: "pro2", Username: "u2", Password: "p2", Enabled: true},
		},
		ProtocolFields: map[string]any{
			"domain":    "example.com",
			"transport": "tcp",
		},
	}

	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("BuildLinks error: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("len(links) = %d, want 2", len(links))
	}
	wantURIs := []string{
		"https://u1:p1@example.com",
		"https://u2:p2@example.com",
	}
	for i, want := range wantURIs {
		if links[i].URI != want {
			t.Errorf("links[%d].URI = %q, want %q", i, links[i].URI, want)
		}
	}
}
