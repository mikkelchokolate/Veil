package hysteria2

import (
	"net/url"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
)

func TestPluginMetadata(t *testing.T) {
	p := New()
	if got, want := p.Protocol(), "hysteria2"; got != want {
		t.Errorf("Protocol() = %q, want %q", got, want)
	}
	if got, want := p.DisplayName(), "Hysteria2"; got != want {
		t.Errorf("DisplayName() = %q, want %q", got, want)
	}
	if got, want := p.Transports(), []string{"udp"}; len(got) != len(want) || got[0] != want[0] {
		t.Errorf("Transports() = %v, want %v", got, want)
	}
	if p.RequiresCaddy() {
		t.Error("RequiresCaddy() = true, want false")
	}
	if got, want := p.FirewallService(), "Veil Hysteria2"; got != want {
		t.Errorf("FirewallService() = %q, want %q", got, want)
	}
	if got, want := p.MaxEnabled(), 0; got != want {
		t.Errorf("MaxEnabled() = %d, want %d", got, want)
	}
}

func TestRenderConfigNoInbounds(t *testing.T) {
	p := New()
	paths := generatedconfig.NewPaths("/tmp/veil")
	artifacts, rendered, err := p.RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{Domain: "example.com"},
		Paths:    paths,
		Inbounds: nil,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rendered {
		t.Error("expected rendered=false for no inbounds")
	}
	if len(artifacts) != 0 {
		t.Fatalf("expected no artifacts, got %d", len(artifacts))
	}
}

func TestRenderConfigWithInbound(t *testing.T) {
	p := New()
	paths := generatedconfig.NewPaths("/tmp/veil")
	artifacts, rendered, err := p.RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{
			Domain:            "example.com",
			Hysteria2Password: "global-secret",
			MasqueradeURL:     "https://example.com",
		},
		Paths: paths,
		Inbounds: []model.Inbound{
			{
				Name:      "h2-1",
				Protocol:  "hysteria2",
				Transport: "udp",
				Port:      8443,
				Enabled:   true,
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rendered {
		t.Error("expected rendered=true")
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	wantPath := paths.Generated("hysteria2/h2-1.yaml")
	if artifacts[0].Path != wantPath {
		t.Errorf("artifact path = %q, want %q", artifacts[0].Path, wantPath)
	}
	for _, want := range []string{
		"listen: :8443",
		"password: global-secret",
		"url: https://example.com",
		"cert: " + paths.PanelCertPath(),
		"key: " + paths.PanelKeyPath(),
	} {
		if !strings.Contains(artifacts[0].Body, want) {
			t.Errorf("rendered config missing %q:\n%s", want, artifacts[0].Body)
		}
	}
}

func TestRenderConfigWithMasqueradeURLFallbackChain(t *testing.T) {
	p := New()
	paths := generatedconfig.NewPaths("/tmp/veil")
	artifacts, _, err := p.RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{
			Domain:        "example.com",
			MasqueradeURL: "https://settings-legacy.example.com",
			ProtocolFields: map[string]any{
				"masqueradeURL": "https://settings-fields.example.com",
			},
		},
		Paths: paths,
		Inbounds: []model.Inbound{
			{
				Name:           "h2-inbound",
				Protocol:       "hysteria2",
				Transport:      "udp",
				Port:           8443,
				Enabled:        true,
				ProtocolFields: map[string]any{"masqueradeURL": "https://inbound-fields.example.com"},
				MasqueradeURL:  "https://inbound-legacy.example.com",
				Password:       "inbound-pass",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(artifacts[0].Body, "url: https://inbound-fields.example.com") {
		t.Errorf("expected inbound ProtocolFields masqueradeURL to win, got:\n%s", artifacts[0].Body)
	}
}

func TestRenderConfigWithPasswordFallbackChain(t *testing.T) {
	p := New()
	paths := generatedconfig.NewPaths("/tmp/veil")
	artifacts, _, err := p.RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{
			Domain:            "example.com",
			Hysteria2Password: "settings-legacy",
			ProtocolFields: map[string]any{
				"hysteria2Password": "settings-fields",
			},
		},
		Paths: paths,
		Inbounds: []model.Inbound{
			{
				Name:              "h2-inbound",
				Protocol:          "hysteria2",
				Transport:         "udp",
				Port:              8443,
				Enabled:           true,
				ProtocolFields:    map[string]any{"hysteria2Password": "inbound-fields"},
				Hysteria2Password: "inbound-legacy",
				Password:          "inbound-pass",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(artifacts[0].Body, "password: inbound-pass") {
		t.Errorf("expected inbound Password to win, got:\n%s", artifacts[0].Body)
	}
}

func TestRenderConfigWithCaddyPanelAccess(t *testing.T) {
	p := New()
	paths := generatedconfig.NewPaths("/tmp/veil")
	artifacts, _, err := p.RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{
			Domain:            "example.com",
			PanelAccess:       "caddy",
			Hysteria2Password: "secret",
		},
		Paths: paths,
		Inbounds: []model.Inbound{
			{
				Name:           "h2-1",
				Protocol:       "hysteria2",
				Transport:      "udp",
				Port:           8443,
				Enabled:        true,
				ProtocolFields: map[string]any{"domain": "example.com"},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(artifacts[0].Body, "cert: "+paths.CertPath("example.com")) {
		t.Errorf("expected caddy cert path, got:\n%s", artifacts[0].Body)
	}
	if !strings.Contains(artifacts[0].Body, "key: "+paths.KeyPath("example.com")) {
		t.Errorf("expected caddy key path, got:\n%s", artifacts[0].Body)
	}
}

func TestRenderConfigWithWarpUpstream(t *testing.T) {
	p := New()
	paths := generatedconfig.NewPaths("/tmp/veil")
	artifacts, _, err := p.RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{
			Domain:            "example.com",
			Hysteria2Password: "secret",
		},
		Paths: paths,
		Inbounds: []model.Inbound{
			{
				Name:      "h2-1",
				Protocol:  "hysteria2",
				Transport: "udp",
				Port:      8443,
				Enabled:   true,
			},
		},
		Warp: model.WarpConfig{Enabled: true, SocksPort: 40001},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(artifacts[0].Body, "addr: 127.0.0.1:40001") {
		t.Errorf("expected warp upstream, got:\n%s", artifacts[0].Body)
	}
}

func TestRenderConfigReturnsErrorForInvalidInbound(t *testing.T) {
	p := New()
	paths := generatedconfig.NewPaths("/tmp/veil")
	_, _, err := p.RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{Domain: "example.com"},
		Paths:    paths,
		Inbounds: []model.Inbound{
			{Name: "h2-bad", Protocol: "hysteria2", Transport: "udp", Port: 0, Enabled: true},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid inbound, got nil")
	}
}

func TestRenderConfigReturnsErrorForProfileMissingPassword(t *testing.T) {
	p := New()
	paths := generatedconfig.NewPaths("/tmp/veil")
	_, _, err := p.RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{Domain: "example.com"},
		Paths:    paths,
		Inbounds: []model.Inbound{
			{
				Name:      "h2-bad",
				Protocol:  "hysteria2",
				Transport: "udp",
				Port:      8443,
				Enabled:   true,
				Profiles:  []model.ClientProfile{{Name: "alice", Enabled: true}},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for profile missing password, got nil")
	}
}

func TestRenderConfigWithWarpDefaultSocksPort(t *testing.T) {
	p := New()
	paths := generatedconfig.NewPaths("/tmp/veil")
	artifacts, _, err := p.RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{
			Domain:            "example.com",
			Hysteria2Password: "secret",
		},
		Paths: paths,
		Inbounds: []model.Inbound{
			{Name: "h2-1", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true},
		},
		Warp: model.WarpConfig{Enabled: true},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(artifacts[0].Body, "addr: 127.0.0.1:40000") {
		t.Errorf("expected default warp socks port, got:\n%s", artifacts[0].Body)
	}
}

func TestRenderConfigWithMultipleInbounds(t *testing.T) {
	p := New()
	paths := generatedconfig.NewPaths("/tmp/veil")
	artifacts, _, err := p.RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{
			Domain:            "example.com",
			Hysteria2Password: "secret",
		},
		Paths: paths,
		Inbounds: []model.Inbound{
			{Name: "h2-a", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true, Password: "pass-a"},
			{Name: "h2-b", Protocol: "hysteria2", Transport: "udp", Port: 8444, Enabled: true, Password: "pass-b"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(artifacts))
	}
	if !strings.Contains(artifacts[0].Body, "listen: :8443") || !strings.Contains(artifacts[0].Body, "password: pass-a") {
		t.Errorf("first artifact mismatch:\n%s", artifacts[0].Body)
	}
	if !strings.Contains(artifacts[1].Body, "listen: :8444") || !strings.Contains(artifacts[1].Body, "password: pass-b") {
		t.Errorf("second artifact mismatch:\n%s", artifacts[1].Body)
	}
}

func TestArtifactSpec(t *testing.T) {
	p := New()
	spec := p.ArtifactSpec()
	if spec.Subpath != generatedconfig.Hysteria2ConfigSubpath {
		t.Errorf("Subpath = %q, want %q", spec.Subpath, generatedconfig.Hysteria2ConfigSubpath)
	}
	if spec.ValidationName != "hysteria2" {
		t.Errorf("ValidationName = %q, want hysteria2", spec.ValidationName)
	}
	if spec.ValidationCommand != nil {
		t.Errorf("ValidationCommand is set, want nil because hysteria has no standalone config checker")
	}
}

func TestRuntimeDescriptorsWithMatchingInbounds(t *testing.T) {
	p := New()
	inbounds := []model.Inbound{
		{Name: "alpha", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true},
		{Name: "beta", Protocol: "hysteria2", Transport: "udp", Port: 8444, Enabled: true},
	}
	runtimes := p.RuntimeDescriptors(inbounds)
	if len(runtimes) != 2 {
		t.Fatalf("expected 2 runtimes, got %d", len(runtimes))
	}
	want := []struct {
		name, unit, subpath string
	}{
		{"hysteria2-alpha", "veil-hysteria2@alpha.service", "hysteria2/alpha.yaml"},
		{"hysteria2-beta", "veil-hysteria2@beta.service", "hysteria2/beta.yaml"},
	}
	for i, w := range want {
		if runtimes[i].Name != w.name {
			t.Errorf("runtimes[%d].Name = %q, want %q", i, runtimes[i].Name, w.name)
		}
		if runtimes[i].Unit != w.unit {
			t.Errorf("runtimes[%d].Unit = %q, want %q", i, runtimes[i].Unit, w.unit)
		}
		if runtimes[i].PromotedSubpath != w.subpath {
			t.Errorf("runtimes[%d].PromotedSubpath = %q, want %q", i, runtimes[i].PromotedSubpath, w.subpath)
		}
		if runtimes[i].TemplateUnit != templateUnit {
			t.Errorf("runtimes[%d].TemplateUnit = %q, want %q", i, runtimes[i].TemplateUnit, templateUnit)
		}
		if runtimes[i].Transport != "udp" {
			t.Errorf("runtimes[%d].Transport = %q, want udp", i, runtimes[i].Transport)
		}
		if !runtimes[i].ManualRestart {
			t.Errorf("runtimes[%d].ManualRestart = false, want true", i)
		}
		if !runtimes[i].HealthCheckAfter {
			t.Errorf("runtimes[%d].HealthCheckAfter = false, want true", i)
		}
	}
}

func TestRuntimeDescriptorsFallbackWhenNoMatchingInbounds(t *testing.T) {
	p := New()
	inbounds := []model.Inbound{
		{Name: "other", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
	}
	runtimes := p.RuntimeDescriptors(inbounds)
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 fallback runtime, got %d", len(runtimes))
	}
	if runtimes[0].Name != "hysteria2" {
		t.Errorf("Name = %q, want hysteria2", runtimes[0].Name)
	}
	if runtimes[0].Unit != templateUnit {
		t.Errorf("Unit = %q, want %q", runtimes[0].Unit, templateUnit)
	}
	if runtimes[0].PromotedSubpath != generatedconfig.Hysteria2ConfigSubpath {
		t.Errorf("PromotedSubpath = %q, want %q", runtimes[0].PromotedSubpath, generatedconfig.Hysteria2ConfigSubpath)
	}
}

func TestRuntimeDescriptorsFiltersOtherProtocols(t *testing.T) {
	p := New()
	inbounds := []model.Inbound{
		{Name: "h2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true},
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
	}
	runtimes := p.RuntimeDescriptors(inbounds)
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(runtimes))
	}
	if runtimes[0].Name != "hysteria2-h2" {
		t.Errorf("Name = %q, want hysteria2-h2", runtimes[0].Name)
	}
}

func TestRuntimeInstall(t *testing.T) {
	p := New()
	rt := p.RuntimeInstall("amd64")
	if rt.Name != "hysteria2" {
		t.Errorf("Name = %q, want hysteria2", rt.Name)
	}
	if rt.Binary != "hysteria" {
		t.Errorf("Binary = %q, want hysteria", rt.Binary)
	}
	if rt.Method != runtimeinstall.MethodRawBinary {
		t.Errorf("Method = %q, want %q", rt.Method, runtimeinstall.MethodRawBinary)
	}
	if rt.Repo != "apernet/hysteria" {
		t.Errorf("Repo = %q, want apernet/hysteria", rt.Repo)
	}
	if rt.AssetMatch == nil {
		t.Fatal("AssetMatch is nil")
	}
	if !rt.AssetMatch("hysteria-linux-amd64") {
		t.Error("AssetMatch should accept hysteria-linux-amd64")
	}
	if rt.AssetMatch("hysteria-linux-arm64") {
		t.Error("AssetMatch should reject arm64 asset on amd64")
	}
	if rt.ChecksumMatch == nil {
		t.Fatal("ChecksumMatch is nil")
	}
	if !rt.ChecksumMatch("hashes.txt") {
		t.Error("ChecksumMatch should accept hashes.txt")
	}
}

func TestValidateSettings(t *testing.T) {
	p := New()
	if err := p.ValidateSettings(model.Settings{}, model.Inbound{}); err != nil {
		t.Errorf("ValidateSettings returned error: %v", err)
	}
}

func TestValidateInbound(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{})
	if len(issues) != 0 {
		t.Errorf("ValidateInbound returned issues: %v", issues)
	}
}

func TestNeedsDomain(t *testing.T) {
	p := New()
	if !p.NeedsDomain(model.Settings{}, model.Inbound{}) {
		t.Error("NeedsDomain() = false, want true")
	}
}

func TestHasCredential(t *testing.T) {
	p := New()

	if p.HasCredential(model.Settings{}, model.Inbound{}) {
		t.Error("HasCredential() = true for empty inputs, want false")
	}

	settings := model.Settings{Hysteria2Password: "global"}
	if !p.HasCredential(settings, model.Inbound{}) {
		t.Error("HasCredential() = false with global password, want true")
	}

	inbound := model.Inbound{ProtocolFields: map[string]any{"hysteria2Password": "inbound"}}
	if !p.HasCredential(model.Settings{}, inbound) {
		t.Error("HasCredential() = false with inbound ProtocolFields password, want true")
	}

	profileInbound := model.Inbound{
		Profiles: []model.ClientProfile{
			{Name: "alice", Password: "secret", Enabled: true},
		},
	}
	if !p.HasCredential(model.Settings{}, profileInbound) {
		t.Error("HasCredential() = false with enabled profile password, want true")
	}

	disabledProfileInbound := model.Inbound{
		Profiles: []model.ClientProfile{
			{Name: "alice", Password: "secret", Enabled: false},
		},
	}
	if p.HasCredential(model.Settings{}, disabledProfileInbound) {
		t.Error("HasCredential() = true with disabled profile, want false")
	}
}

func TestInboundFieldSchema(t *testing.T) {
	p := New()
	fields := p.InboundFieldSchema()
	if len(fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(fields))
	}
	keys := map[string]schema.FieldType{
		"hysteria2Password": schema.FieldPassword,
		"masqueradeURL":     schema.FieldText,
		"hysteria2Insecure": schema.FieldCheckbox,
	}
	for _, f := range fields {
		wantType, ok := keys[f.Key]
		if !ok {
			t.Errorf("unexpected field key %q", f.Key)
			continue
		}
		if f.Type != wantType {
			t.Errorf("field %q type = %q, want %q", f.Key, f.Type, wantType)
		}
		if f.Scope != "inbound" {
			t.Errorf("field %q scope = %q, want inbound", f.Key, f.Scope)
		}
	}
}

func TestSettingsFieldSchema(t *testing.T) {
	p := New()
	fields := p.SettingsFieldSchema()
	if len(fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(fields))
	}
	for _, f := range fields {
		if f.Scope != "settings" {
			t.Errorf("field %q scope = %q, want settings", f.Key, f.Scope)
		}
	}
}

func TestAutofill(t *testing.T) {
	p := New()
	inbound := model.Inbound{Name: "h2"}
	out, err := p.Autofill(inbound)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Name != inbound.Name {
		t.Error("Autofill should return inbound unchanged")
	}
}

func TestBuildLinksNoDomain(t *testing.T) {
	p := New()
	links, err := p.BuildLinks(model.Settings{}, model.Inbound{Name: "h2", Protocol: "hysteria2", Port: 8443})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("expected no links without domain, got %d", len(links))
	}
}

func TestBuildLinksWithPasswordFallback(t *testing.T) {
	p := New()
	settings := model.Settings{Domain: "example.com"}
	inbound := model.Inbound{
		Name:      "h2",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      8443,
		Password:  "secret-pass",
	}
	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	link := links[0]
	if link.Name != "h2" {
		t.Errorf("Name = %q, want h2", link.Name)
	}
	if link.Protocol != "hysteria2" {
		t.Errorf("Protocol = %q, want hysteria2", link.Protocol)
	}
	if link.Port != 8443 {
		t.Errorf("Port = %d, want 8443", link.Port)
	}
	wantPrefix := "hysteria2://" + url.QueryEscape("secret-pass") + "@example.com:8443/"
	if !strings.HasPrefix(link.URI, wantPrefix) {
		t.Errorf("URI prefix mismatch: got %q", link.URI)
	}
	if !strings.Contains(link.URI, "sni=example.com") {
		t.Errorf("URI missing sni: %q", link.URI)
	}
	if strings.Contains(link.URI, "insecure=") {
		t.Errorf("URI should not contain insecure when flag is false: %q", link.URI)
	}
}

func TestBuildLinksWithInsecureFlag(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:            "example.com",
		Hysteria2Insecure: true,
	}
	inbound := model.Inbound{
		Name:      "h2",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      8443,
		Password:  "secret-pass",
	}
	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if !strings.Contains(links[0].URI, "insecure=1") {
		t.Errorf("URI missing insecure flag: %q", links[0].URI)
	}
}

func TestBuildLinksWithProfiles(t *testing.T) {
	p := New()
	settings := model.Settings{Domain: "example.com"}
	inbound := model.Inbound{
		Name:      "h2",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      8443,
		Profiles: []model.ClientProfile{
			{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: true},
			{Name: "bob", Username: "bob", Password: "bob-pass", Enabled: true},
			{Name: "carol", Password: "ignored", Enabled: false},
		},
	}
	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	if links[0].Name != "h2/alice" {
		t.Errorf("link[0].Name = %q, want h2/alice", links[0].Name)
	}
	if !strings.Contains(links[0].URI, "alice:alice-pass") {
		t.Errorf("link[0].URI missing alice credentials: %q", links[0].URI)
	}
	if links[1].Name != "h2/bob" {
		t.Errorf("link[1].Name = %q, want h2/bob", links[1].Name)
	}
	if !strings.Contains(links[1].URI, "bob:bob-pass") {
		t.Errorf("link[1].URI missing bob credentials: %q", links[1].URI)
	}
}

func TestBuildLinksFallbackToSettingsPassword(t *testing.T) {
	p := New()
	settings := model.Settings{
		Domain:            "example.com",
		Hysteria2Password: "settings-pass",
	}
	inbound := model.Inbound{
		Name:      "h2",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      8443,
	}
	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 fallback link, got %d", len(links))
	}
	if !strings.Contains(links[0].URI, "settings-pass") {
		t.Errorf("fallback URI missing settings password: %q", links[0].URI)
	}
}

func TestBuildLinksProfileRequiresPassword(t *testing.T) {
	p := New()
	settings := model.Settings{Domain: "example.com"}
	inbound := model.Inbound{
		Name:      "h2",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      8443,
		Password:  "fallback-pass",
		Profiles: []model.ClientProfile{
			{Name: "alice", Enabled: true},
		},
	}
	_, err := p.BuildLinks(settings, inbound)
	if err == nil {
		t.Fatal("expected error for enabled profile missing password, got nil")
	}
	if !strings.Contains(err.Error(), "username and password are required") {
		t.Errorf("expected username/password required error, got: %v", err)
	}
}

func TestHysteria2DomainFromProtocolFields(t *testing.T) {
	inbound := model.Inbound{
		Protocol:       "hysteria2",
		ProtocolFields: map[string]any{"domain": "hy.example.com"},
	}
	if got := Hysteria2Domain(inbound); got != "hy.example.com" {
		t.Errorf("domain = %q", got)
	}
}

func TestProtocolString(t *testing.T) {
	if got := protocolString(nil, "k", "fallback"); got != "fallback" {
		t.Errorf("nil map = %q, want fallback", got)
	}
	if got := protocolString(map[string]any{"k": " value "}, "k", "fallback"); got != "value" {
		t.Errorf("trimmed value = %q, want value", got)
	}
	if got := protocolString(map[string]any{"k": 123}, "k", "fallback"); got != "fallback" {
		t.Errorf("non-string = %q, want fallback", got)
	}
	if got := protocolString(map[string]any{}, "missing", "fallback"); got != "fallback" {
		t.Errorf("missing key = %q, want fallback", got)
	}
}

func TestProtocolBool(t *testing.T) {
	if got := protocolBool(nil, "k", true); !got {
		t.Error("nil map should return fallback true")
	}
	if got := protocolBool(map[string]any{}, "k", false); got {
		t.Error("missing key should return fallback false")
	}
	if got := protocolBool(map[string]any{"k": true}, "k", false); !got {
		t.Error("true value should return true")
	}
	if got := protocolBool(map[string]any{"k": false}, "k", true); got {
		t.Error("false value should return false")
	}
	if got := protocolBool(map[string]any{"k": "true"}, "k", true); !got {
		t.Error("non-bool should return fallback")
	}
}

func TestHysteria2InsecureFallbackChain(t *testing.T) {
	p := New()

	// Inbound legacy field wins over everything.
	settings := model.Settings{Domain: "example.com"}
	inbound := model.Inbound{
		Name:              "h2",
		Protocol:          "hysteria2",
		Transport:         "udp",
		Port:              8443,
		Password:          "secret",
		Hysteria2Insecure: true,
	}
	links, err := p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(links[0].URI, "insecure=1") {
		t.Errorf("inbound legacy insecure should win: %q", links[0].URI)
	}

	// Inbound ProtocolFields wins.
	settings = model.Settings{Domain: "example.com"}
	inbound = model.Inbound{
		Name:              "h2",
		Protocol:          "hysteria2",
		Transport:         "udp",
		Port:              8443,
		Password:          "secret",
		Hysteria2Insecure: false,
		ProtocolFields:    map[string]any{"hysteria2Insecure": true},
	}
	links, err = p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(links[0].URI, "insecure=1") {
		t.Errorf("inbound ProtocolFields insecure should win: %q", links[0].URI)
	}

	// Settings legacy field.
	settings = model.Settings{Domain: "example.com", Hysteria2Insecure: true}
	inbound = model.Inbound{Name: "h2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Password: "secret"}
	links, err = p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(links[0].URI, "insecure=1") {
		t.Errorf("settings legacy insecure should win: %q", links[0].URI)
	}

	// Settings ProtocolFields.
	settings = model.Settings{Domain: "example.com", ProtocolFields: map[string]any{"hysteria2Insecure": true}}
	inbound = model.Inbound{Name: "h2", Protocol: "hysteria2", Transport: "udp", Port: 8443, Password: "secret"}
	links, err = p.BuildLinks(settings, inbound)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(links[0].URI, "insecure=1") {
		t.Errorf("settings ProtocolFields insecure should win: %q", links[0].URI)
	}
}
