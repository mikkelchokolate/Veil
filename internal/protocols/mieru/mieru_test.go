package mieru

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestPluginMetadata(t *testing.T) {
	p := New()
	if got, want := p.Protocol(), "mieru"; got != want {
		t.Errorf("Protocol() = %q, want %q", got, want)
	}
	if got, want := p.DisplayName(), "Mieru"; got != want {
		t.Errorf("DisplayName() = %q, want %q", got, want)
	}
	if got, want := p.Transports(), []string{"tcp", "udp"}; !slicesEqual(got, want) {
		t.Errorf("Transports() = %v, want %v", got, want)
	}
	if p.RequiresCaddy() {
		t.Error("RequiresCaddy() = true, want false")
	}
	if got, want := p.FirewallService(), "Veil Mieru"; got != want {
		t.Errorf("FirewallService() = %q, want %q", got, want)
	}
	if p.MaxEnabled() != 0 {
		t.Errorf("MaxEnabled() = %d, want 0", p.MaxEnabled())
	}
}

func TestRenderConfigTCPInbound(t *testing.T) {
	p := New()
	applyRoot := t.TempDir()
	input := generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{Domain: "example.com"},
		Paths:    generatedconfig.NewPaths(applyRoot),
		Inbounds: []model.Inbound{
			{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "tcp-pass"},
		},
	}

	artifacts, ok, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	if !ok {
		t.Fatal("RenderConfig ok = false, want true")
	}
	if len(artifacts) != 1 {
		t.Fatalf("len(artifacts) = %d, want 1", len(artifacts))
	}
	wantPath := generatedconfig.NewPaths(applyRoot).Mieru()
	if artifacts[0].Path != wantPath {
		t.Errorf("artifact.Path = %q, want %q", artifacts[0].Path, wantPath)
	}

	decoded := decodeMieruServerConfig(t, artifacts[0].Body)
	if len(decoded.PortBindings) != 1 || decoded.PortBindings[0].Port != 443 || decoded.PortBindings[0].Protocol != "TCP" {
		t.Errorf("port bindings = %+v", decoded.PortBindings)
	}
	if len(decoded.Users) != 1 || decoded.Users[0].Name != "mieru-tcp" || decoded.Users[0].Password != "tcp-pass" {
		t.Errorf("users = %+v", decoded.Users)
	}
}

func TestRenderConfigUDPInbound(t *testing.T) {
	p := New()
	applyRoot := t.TempDir()
	input := generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{Domain: "example.com"},
		Paths:    generatedconfig.NewPaths(applyRoot),
		Inbounds: []model.Inbound{
			{
				Name:      "mieru-udp",
				Protocol:  "mieru",
				Transport: "udp",
				Port:      8443,
				Enabled:   true,
				Profiles: []model.ClientProfile{
					{Name: "alice", Password: "alice-pass", Enabled: true},
				},
			},
		},
	}

	artifacts, ok, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	if !ok || len(artifacts) != 1 {
		t.Fatalf("ok=%v, len(artifacts)=%d", ok, len(artifacts))
	}

	decoded := decodeMieruServerConfig(t, artifacts[0].Body)
	if len(decoded.PortBindings) != 1 || decoded.PortBindings[0].Port != 8443 || decoded.PortBindings[0].Protocol != "UDP" {
		t.Errorf("port bindings = %+v", decoded.PortBindings)
	}
	if len(decoded.Users) != 1 || decoded.Users[0].Name != "alice" || decoded.Users[0].Password != "alice-pass" {
		t.Errorf("users = %+v", decoded.Users)
	}
}

func TestRenderConfigMultipleInboundsAggregated(t *testing.T) {
	p := New()
	applyRoot := t.TempDir()
	input := generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{Domain: "example.com"},
		Paths:    generatedconfig.NewPaths(applyRoot),
		Inbounds: []model.Inbound{
			{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "tcp-pass"},
			{
				Name:      "mieru-udp",
				Protocol:  "mieru",
				Transport: "udp",
				Port:      8443,
				Enabled:   true,
				Profiles:  []model.ClientProfile{{Name: "alice", Password: "alice-pass", Enabled: true}},
			},
		},
	}

	artifacts, ok, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	if !ok || len(artifacts) != 1 {
		t.Fatalf("ok=%v, len(artifacts)=%d", ok, len(artifacts))
	}

	decoded := decodeMieruServerConfig(t, artifacts[0].Body)
	if len(decoded.PortBindings) != 2 {
		t.Fatalf("port bindings = %+v", decoded.PortBindings)
	}
	protocols := map[string]int{}
	for _, b := range decoded.PortBindings {
		protocols[b.Protocol]++
	}
	if protocols["TCP"] != 1 || protocols["UDP"] != 1 {
		t.Errorf("protocol counts = %v", protocols)
	}
	if len(decoded.Users) != 2 {
		t.Fatalf("users = %+v", decoded.Users)
	}
	usernames := map[string]bool{}
	for _, u := range decoded.Users {
		usernames[u.Name] = true
	}
	if !usernames["mieru-tcp"] || !usernames["alice"] {
		t.Errorf("unexpected users = %+v", decoded.Users)
	}
}

func TestRenderConfigEmptyInbounds(t *testing.T) {
	p := New()
	applyRoot := t.TempDir()
	input := generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{},
		Paths:    generatedconfig.NewPaths(applyRoot),
		Inbounds: nil,
	}

	artifacts, ok, err := p.RenderConfig(input)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	if ok {
		t.Error("ok = true, want false")
	}
	if artifacts != nil {
		t.Errorf("artifacts = %v, want nil", artifacts)
	}
}

func TestRenderConfigReturnsErrorWhenNoUser(t *testing.T) {
	p := New()
	applyRoot := t.TempDir()
	input := generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{},
		Paths:    generatedconfig.NewPaths(applyRoot),
		Inbounds: []model.Inbound{
			{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true},
		},
	}

	artifacts, _, err := p.RenderConfig(input)
	if err == nil {
		t.Fatal("RenderConfig expected error, got nil")
	}
	if artifacts != nil {
		t.Errorf("artifacts = %v, want nil", artifacts)
	}
}

func TestArtifactSpec(t *testing.T) {
	p := New()
	spec := p.ArtifactSpec()
	if spec.Subpath != generatedconfig.MieruConfigSubpath {
		t.Errorf("Subpath = %q, want %q", spec.Subpath, generatedconfig.MieruConfigSubpath)
	}
	if spec.ValidationName != "mieru" {
		t.Errorf("ValidationName = %q, want %q", spec.ValidationName, "mieru")
	}
	if spec.ValidationCommand != nil {
		t.Fatalf("ValidationCommand = %v, want nil because mita has no standalone config checker", spec.ValidationCommand("/tmp/config.json"))
	}
}

func TestRuntimeDescriptors(t *testing.T) {
	p := New()
	want := service.ManagedRuntime{
		Name:             "mieru",
		ActionName:       "mieru",
		Protocol:         "mieru",
		Unit:             "veil-mieru.service",
		PromotedSubpath:  "mieru/server_config.json",
		PromotedVerb:     "restart",
		ManualRestart:    true,
		HealthCheckAfter: true,
	}

	tests := []struct {
		name     string
		inbounds []model.Inbound
	}{
		{
			name: "with enabled mieru inbound",
			inbounds: []model.Inbound{
				{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "p"},
			},
		},
		{
			name:     "without enabled mieru inbound",
			inbounds: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtimes := p.RuntimeDescriptors(tt.inbounds)
			if len(runtimes) != 1 {
				t.Fatalf("len(runtimes) = %d, want 1", len(runtimes))
			}
			if runtimes[0] != want {
				t.Errorf("runtime = %+v, want %+v", runtimes[0], want)
			}
		})
	}
}

func TestRuntimeInstall(t *testing.T) {
	p := New()
	ri := p.RuntimeInstall("amd64")
	if ri.Name != "mieru" {
		t.Errorf("Name = %q, want %q", ri.Name, "mieru")
	}
	if ri.Binary != "mita" {
		t.Errorf("Binary = %q, want %q", ri.Binary, "mita")
	}
	if ri.Method != runtimeinstall.MethodArchive {
		t.Errorf("Method = %v, want %v", ri.Method, runtimeinstall.MethodArchive)
	}
	if ri.Repo != "enfein/mieru" {
		t.Errorf("Repo = %q, want %q", ri.Repo, "enfein/mieru")
	}

	valid := "mita_3.0.0_linux_amd64.tar.gz"
	if !ri.AssetMatch(valid) {
		t.Errorf("AssetMatch(%q) = false, want true", valid)
	}
	if !ri.ChecksumMatch(valid + ".sha256.txt") {
		t.Errorf("ChecksumMatch(%q) = false, want true", valid+".sha256.txt")
	}
	invalid := []string{
		"mita_3.0.0_linux_arm64.tar.gz",
		"mita_3.0.0_linux_amd64.zip",
		"other_3.0.0_linux_amd64.tar.gz",
	}
	for _, name := range invalid {
		if ri.AssetMatch(name) {
			t.Errorf("AssetMatch(%q) = true, want false", name)
		}
	}
}

func TestValidateSettings(t *testing.T) {
	p := New()
	if err := p.ValidateSettings(model.Settings{}, model.Inbound{}); err != nil {
		t.Errorf("ValidateSettings error = %v, want nil", err)
	}
}

func TestValidateInbound(t *testing.T) {
	p := New()
	issues := p.ValidateInbound(model.Settings{}, model.Inbound{})
	if issues != nil {
		t.Errorf("ValidateInbound = %v, want nil", issues)
	}
}

func TestNeedsDomain(t *testing.T) {
	p := New()
	if p.NeedsDomain(model.Settings{}, model.Inbound{}) {
		t.Error("NeedsDomain = true, want false")
	}
}

func TestHasCredential(t *testing.T) {
	p := New()

	tests := []struct {
		name    string
		inbound model.Inbound
		want    bool
	}{
		{
			name: "enabled profile with password",
			inbound: model.Inbound{
				Profiles: []model.ClientProfile{
					{Name: "alice", Password: "secret", Enabled: true},
				},
			},
			want: true,
		},
		{
			name: "disabled profile without inbound password",
			inbound: model.Inbound{
				Profiles: []model.ClientProfile{
					{Name: "alice", Password: "secret", Enabled: false},
				},
			},
			want: false,
		},
		{
			name:    "inbound password",
			inbound: model.Inbound{Password: "secret"},
			want:    true,
		},
		{
			name: "protocol fields password",
			inbound: model.Inbound{
				ProtocolFields: map[string]any{"password": "secret"},
			},
			want: true,
		},
		{
			name: "empty password falls back to protocol fields",
			inbound: model.Inbound{
				Password:       "   ",
				ProtocolFields: map[string]any{"password": "fields-pass"},
			},
			want: true,
		},
		{
			name: "whitespace only password",
			inbound: model.Inbound{
				Password: "   ",
			},
			want: false,
		},
		{
			name: "protocol fields password is non-string",
			inbound: model.Inbound{
				Password:       "",
				ProtocolFields: map[string]any{"password": 123},
			},
			want: false,
		},
		{
			name: "no credential",
			inbound: model.Inbound{
				ProtocolFields: map[string]any{"other": "value"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.HasCredential(model.Settings{}, tt.inbound); got != tt.want {
				t.Errorf("HasCredential = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUIProviderSchemas(t *testing.T) {
	p := New()
	if got := p.InboundFieldSchema(); got != nil {
		t.Errorf("InboundFieldSchema = %v, want nil", got)
	}
	if got := p.SettingsFieldSchema(); got != nil {
		t.Errorf("SettingsFieldSchema = %v, want nil", got)
	}
}

func TestUIProviderAutofill(t *testing.T) {
	p := New()
	inbound := model.Inbound{Name: "mieru-tcp", Protocol: "mieru"}
	out, err := p.Autofill(inbound)
	if err != nil {
		t.Fatalf("Autofill: %v", err)
	}
	if out.Name != inbound.Name || out.Protocol != inbound.Protocol {
		t.Errorf("Autofill returned %+v, want %+v", out, inbound)
	}
}

func TestBuildLinks(t *testing.T) {
	p := New()
	links, err := p.BuildLinks(model.Settings{}, model.Inbound{})
	if err != nil {
		t.Fatalf("BuildLinks: %v", err)
	}
	if links != nil {
		t.Errorf("BuildLinks = %v, want nil", links)
	}
}

func TestAggregateLinksBuildsConfigAndURI(t *testing.T) {
	p := New()
	settings := model.Settings{Domain: "example.com"}
	inbounds := []model.Inbound{
		{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "tcp-pass"},
	}

	links, err := p.AggregateLinks(settings, inbounds)
	if err != nil {
		t.Fatalf("AggregateLinks: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("len(links) = %d, want 1", len(links))
	}
	link := links[0]
	if link.Name != "mieru-tcp" {
		t.Errorf("Name = %q, want %q", link.Name, "mieru-tcp")
	}
	if link.Protocol != "mieru" {
		t.Errorf("Protocol = %q, want %q", link.Protocol, "mieru")
	}
	if link.Transport != "tcp" {
		t.Errorf("Transport = %q, want %q", link.Transport, "tcp")
	}
	if link.Port != 443 {
		t.Errorf("Port = %d, want 443", link.Port)
	}
	if !strings.HasPrefix(link.URI, "mierus://") {
		t.Errorf("URI = %q, want mierus:// prefix", link.URI)
	}
	if !strings.Contains(link.URI, "profile=mieru-tcp") {
		t.Errorf("URI missing profile: %q", link.URI)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(link.Config), &decoded); err != nil {
		t.Fatalf("invalid client config JSON: %v\n%s", err, link.Config)
	}
	if decoded["activeProfile"] != "mieru-tcp" {
		t.Errorf("activeProfile = %v, want %q", decoded["activeProfile"], "mieru-tcp")
	}
}

func TestAggregateLinksWithoutDomainReturnsNil(t *testing.T) {
	p := New()
	inbounds := []model.Inbound{
		{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "tcp-pass"},
	}

	links, err := p.AggregateLinks(model.Settings{}, inbounds)
	if err != nil {
		t.Fatalf("AggregateLinks: %v", err)
	}
	if links != nil {
		t.Errorf("links = %v, want nil", links)
	}
}

func TestAggregateLinksWithProfiles(t *testing.T) {
	p := New()
	settings := model.Settings{Domain: "example.com"}
	inbounds := []model.Inbound{
		{
			Name:      "mieru-udp",
			Protocol:  "mieru",
			Transport: "udp",
			Port:      8443,
			Enabled:   true,
			Profiles: []model.ClientProfile{
				{Name: "alice", Password: "alice-pass", Enabled: true},
			},
		},
	}

	links, err := p.AggregateLinks(settings, inbounds)
	if err != nil {
		t.Fatalf("AggregateLinks: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("len(links) = %d, want 1", len(links))
	}
	if links[0].Name != "mieru/alice" {
		t.Errorf("Name = %q, want %q", links[0].Name, "mieru/alice")
	}
	if links[0].Transport != "udp" {
		t.Errorf("Transport = %q, want udp", links[0].Transport)
	}
}

func decodeMieruServerConfig(t *testing.T, body string) struct {
	PortBindings []struct {
		Port     int    `json:"port"`
		Protocol string `json:"protocol"`
	} `json:"portBindings"`
	Users []struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	} `json:"users"`
} {
	t.Helper()
	var decoded struct {
		PortBindings []struct {
			Port     int    `json:"port"`
			Protocol string `json:"protocol"`
		} `json:"portBindings"`
		Users []struct {
			Name     string `json:"name"`
			Password string `json:"password"`
		} `json:"users"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("invalid Mieru JSON: %v\n%s", err, body)
	}
	return decoded
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
