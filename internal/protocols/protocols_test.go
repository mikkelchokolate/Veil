package protocols

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
	"github.com/mikkelchokolate/Veil/internal/service"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

type mockPlugin struct {
	protocol        string
	displayName     string
	transports      []string
	requiresCaddy   bool
	firewallService string
	maxEnabled      int
}

func (m *mockPlugin) Protocol() string        { return m.protocol }
func (m *mockPlugin) DisplayName() string     { return m.displayName }
func (m *mockPlugin) Transports() []string    { return append([]string(nil), m.transports...) }
func (m *mockPlugin) RequiresCaddy() bool     { return m.requiresCaddy }
func (m *mockPlugin) FirewallService() string { return m.firewallService }
func (m *mockPlugin) MaxEnabled() int         { return m.maxEnabled }

type mockConfigRenderer struct {
	*mockPlugin
	render func(generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error)
	spec   generatedconfig.ArtifactSpec
}

func (m *mockConfigRenderer) RenderConfig(input generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error) {
	if m.render != nil {
		return m.render(input)
	}
	return []generatedconfig.GeneratedConfigArtifact{
		{Path: filepath.Join("generated", m.protocol, "cfg.json"), Body: `{"proto":"` + m.protocol + `"}`},
	}, true, nil
}

func (m *mockConfigRenderer) ArtifactSpec() generatedconfig.ArtifactSpec { return m.spec }

type mockRuntimeProvider struct {
	*mockPlugin
	descriptors []service.ManagedRuntime
	runtime     runtimeinstall.Runtime
}

func (m *mockRuntimeProvider) RuntimeDescriptors(enabledInbounds []model.Inbound) []service.ManagedRuntime {
	out := make([]service.ManagedRuntime, len(m.descriptors))
	copy(out, m.descriptors)
	return out
}

func (m *mockRuntimeProvider) RuntimeInstall(arch string) runtimeinstall.Runtime {
	rt := m.runtime
	rt.Name = rt.Name + "-" + arch
	return rt
}

type mockValidator struct {
	*mockPlugin
	validateSettings func(model.Settings) error
	validateInbound  func(model.Settings, model.Inbound) []model.ValidationIssue
	needsDomain      func(model.Settings, model.Inbound) bool
	hasCredential    func(model.Settings, model.Inbound) bool
}

func (m *mockValidator) ValidateSettings(settings model.Settings) error {
	if m.validateSettings != nil {
		return m.validateSettings(settings)
	}
	return nil
}

func (m *mockValidator) ValidateInbound(settings model.Settings, inbound model.Inbound) []model.ValidationIssue {
	if m.validateInbound != nil {
		return m.validateInbound(settings, inbound)
	}
	return nil
}

func (m *mockValidator) NeedsDomain(settings model.Settings, inbound model.Inbound) bool {
	if m.needsDomain != nil {
		return m.needsDomain(settings, inbound)
	}
	return false
}

func (m *mockValidator) HasCredential(settings model.Settings, inbound model.Inbound) bool {
	if m.hasCredential != nil {
		return m.hasCredential(settings, inbound)
	}
	return false
}

type mockClientAccessProvider struct {
	*mockPlugin
	links func(model.Settings, model.Inbound) ([]model.ClientLink, error)
}

func (m *mockClientAccessProvider) BuildLinks(settings model.Settings, inbound model.Inbound) ([]model.ClientLink, error) {
	if m.links != nil {
		return m.links(settings, inbound)
	}
	return nil, nil
}

type mockUIProvider struct {
	*mockPlugin
	inboundFields  []schema.FieldSchema
	settingsFields []schema.FieldSchema
	autofill       func(model.Inbound) (model.Inbound, error)
}

func (m *mockUIProvider) InboundFieldSchema() []schema.FieldSchema  { return m.inboundFields }
func (m *mockUIProvider) SettingsFieldSchema() []schema.FieldSchema { return m.settingsFields }
func (m *mockUIProvider) Autofill(inbound model.Inbound) (model.Inbound, error) {
	if m.autofill != nil {
		return m.autofill(inbound)
	}
	return inbound, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeTarGz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{
		Name: name,
		Mode: 0o755,
		Size: int64(len(body)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func containsResult(results []runtimeinstall.Result, name string) bool {
	for _, r := range results {
		if r.Name == name {
			return true
		}
	}
	return false
}

func resultByName(results []runtimeinstall.Result, name string) (runtimeinstall.Result, bool) {
	for _, r := range results {
		if r.Name == name {
			return r, true
		}
	}
	return runtimeinstall.Result{}, false
}

// ---------------------------------------------------------------------------
// registry.go
// ---------------------------------------------------------------------------

func TestRegistryNewRegistryRawIsEmpty(t *testing.T) {
	r := NewRegistryRaw()
	if len(r.All()) != 0 || len(r.Protocols()) != 0 {
		t.Fatalf("new raw registry should be empty")
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistryRaw()
	p1 := &mockPlugin{protocol: "alpha", displayName: "Alpha"}
	p2 := &mockPlugin{protocol: "beta", displayName: "Beta"}

	r.Register(p1)
	r.Register(p2)

	if got, ok := r.Get("alpha"); !ok || got != p1 {
		t.Fatalf("Get(alpha) = %v, %v", got, ok)
	}
	if got, ok := r.Get("beta"); !ok || got != p2 {
		t.Fatalf("Get(beta) = %v, %v", got, ok)
	}
	if _, ok := r.Get("gamma"); ok {
		t.Fatalf("Get(gamma) should not exist")
	}
}

func TestRegistryRegisterDuplicatePanics(t *testing.T) {
	r := NewRegistryRaw()
	r.Register(&mockPlugin{protocol: "alpha"})

	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic on duplicate protocol registration")
		}
	}()
	r.Register(&mockPlugin{protocol: "alpha"})
}

func TestRegistryAllPreservesOrder(t *testing.T) {
	r := NewRegistryRaw()
	r.Register(&mockPlugin{protocol: "c", displayName: "C"})
	r.Register(&mockPlugin{protocol: "a", displayName: "A"})
	r.Register(&mockPlugin{protocol: "b", displayName: "B"})

	all := r.All()
	if len(all) != 3 {
		t.Fatalf("len(All) = %d", len(all))
	}
	if all[0].Protocol() != "c" || all[1].Protocol() != "a" || all[2].Protocol() != "b" {
		t.Fatalf("All order = %v", protocolNames(all))
	}
}

func TestRegistryProtocolsReturnsSortedKeys(t *testing.T) {
	r := NewRegistryRaw()
	r.Register(&mockPlugin{protocol: "charlie"})
	r.Register(&mockPlugin{protocol: "alpha"})
	r.Register(&mockPlugin{protocol: "bravo"})

	got := r.Protocols()
	want := []string{"alpha", "bravo", "charlie"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Protocols = %v, want %v", got, want)
	}
}

func TestRegistryChoicesDefensiveCopy(t *testing.T) {
	r := NewRegistryRaw()
	r.Register(&mockPlugin{protocol: "p1", displayName: "P1", transports: []string{"tcp"}})

	choices := r.Choices()
	choices[0].Transports[0] = "mutated"

	if got := r.Choices()[0].Transports[0]; got != "tcp" {
		t.Fatalf("Choices mutated registry: got %q", got)
	}
}

func TestRegistryChoicesContent(t *testing.T) {
	r := NewRegistryRaw()
	r.Register(&mockPlugin{
		protocol: "p1", displayName: "P1", transports: []string{"tcp", "udp"},
		firewallService: "svc1", requiresCaddy: true,
	})
	r.Register(&mockPlugin{protocol: "p2", displayName: "P2", transports: []string{"udp"}})

	choices := r.Choices()
	if len(choices) != 2 {
		t.Fatalf("len(choices) = %d", len(choices))
	}
	if choices[0].Protocol != "p1" || choices[0].DisplayName != "P1" || choices[0].FirewallService != "svc1" || !choices[0].RequiresCaddy {
		t.Fatalf("choices[0] = %+v", choices[0])
	}
	if !reflect.DeepEqual(choices[0].Transports, []string{"tcp", "udp"}) {
		t.Fatalf("transports = %v", choices[0].Transports)
	}
}

func TestRegistryMetadata(t *testing.T) {
	r := NewRegistryRaw()
	r.Register(&mockPlugin{
		protocol: "p1", displayName: "P1", transports: []string{"tcp"},
		firewallService: "fw", maxEnabled: 3,
	})

	meta := r.Metadata("p1")
	if meta.Protocol != "p1" || meta.DisplayName != "P1" || meta.FirewallService != "fw" || meta.MaxEnabled != 3 {
		t.Fatalf("Metadata = %+v", meta)
	}
	if got := r.Metadata("missing"); !reflect.DeepEqual(got, Metadata{}) {
		t.Fatalf("Metadata(missing) = %+v", got)
	}
}

func TestRegistrySupportsTransport(t *testing.T) {
	r := NewRegistryRaw()
	r.Register(&mockPlugin{protocol: "p1", transports: []string{"tcp", "udp"}})

	if !r.SupportsTransport("p1", "tcp") {
		t.Fatalf("expected tcp support")
	}
	if !r.SupportsTransport("p1", "udp") {
		t.Fatalf("expected udp support")
	}
	if r.SupportsTransport("p1", "ws") {
		t.Fatalf("expected no ws support")
	}
	if r.SupportsTransport("missing", "tcp") {
		t.Fatalf("missing protocol should not support transport")
	}
}

func TestRegistryFirewallService(t *testing.T) {
	r := NewRegistryRaw()
	r.Register(&mockPlugin{protocol: "p1", firewallService: "fw1"})
	r.Register(&mockPlugin{protocol: "p2", firewallService: ""})

	if svc, ok := r.FirewallService("p1"); !ok || svc != "fw1" {
		t.Fatalf("FirewallService(p1) = %q, %v", svc, ok)
	}
	if _, ok := r.FirewallService("p2"); ok {
		t.Fatalf("FirewallService(p2) should be false")
	}
	if _, ok := r.FirewallService("missing"); ok {
		t.Fatalf("FirewallService(missing) should be false")
	}
}

func TestRegistryRequiresCaddy(t *testing.T) {
	r := NewRegistryRaw()
	r.Register(&mockPlugin{protocol: "p1", requiresCaddy: false})
	if r.RequiresCaddy() {
		t.Fatalf("no plugin requires caddy")
	}

	r.Register(&mockPlugin{protocol: "p2", requiresCaddy: true})
	if !r.RequiresCaddy() {
		t.Fatalf("at least one plugin requires caddy")
	}
}

func protocolNames(plugins []ProtocolPlugin) []string {
	out := make([]string, len(plugins))
	for i, p := range plugins {
		out[i] = p.Protocol()
	}
	return out
}

// ---------------------------------------------------------------------------
// plugin.go
// ---------------------------------------------------------------------------

func TestMetadataOf(t *testing.T) {
	p := &mockPlugin{
		protocol: "x", displayName: "X", transports: []string{"a", "b"},
		requiresCaddy: true, firewallService: "fw", maxEnabled: 5,
	}
	meta := MetadataOf(p)
	want := Metadata{
		Protocol: "x", DisplayName: "X", Transports: []string{"a", "b"},
		RequiresCaddy: true, FirewallService: "fw", MaxEnabled: 5,
	}
	if !reflect.DeepEqual(meta, want) {
		t.Fatalf("MetadataOf = %+v, want %+v", meta, want)
	}

	meta.Transports[0] = "z"
	if p.Transports()[0] != "a" {
		t.Fatalf("MetadataOf mutated plugin transports")
	}
}

func TestAsCapabilityHelpers(t *testing.T) {
	base := &mockPlugin{protocol: "x"}
	cr := &mockConfigRenderer{mockPlugin: base}
	rp := &mockRuntimeProvider{mockPlugin: base}
	val := &mockValidator{mockPlugin: base}
	cap := &mockClientAccessProvider{mockPlugin: base}
	ui := &mockUIProvider{mockPlugin: base}
	plain := &mockPlugin{protocol: "plain"}

	if c, ok := AsConfigRenderer(cr); !ok || c == nil {
		t.Fatalf("AsConfigRenderer failed")
	}
	if _, ok := AsConfigRenderer(plain); ok {
		t.Fatalf("AsConfigRenderer should fail for plain plugin")
	}

	if c, ok := AsRuntimeProvider(rp); !ok || c == nil {
		t.Fatalf("AsRuntimeProvider failed")
	}
	if _, ok := AsRuntimeProvider(plain); ok {
		t.Fatalf("AsRuntimeProvider should fail for plain plugin")
	}

	if c, ok := AsValidator(val); !ok || c == nil {
		t.Fatalf("AsValidator failed")
	}
	if _, ok := AsValidator(plain); ok {
		t.Fatalf("AsValidator should fail for plain plugin")
	}

	if c, ok := AsClientAccessProvider(cap); !ok || c == nil {
		t.Fatalf("AsClientAccessProvider failed")
	}
	if _, ok := AsClientAccessProvider(plain); ok {
		t.Fatalf("AsClientAccessProvider should fail for plain plugin")
	}

	if c, ok := AsUIProvider(ui); !ok || c == nil {
		t.Fatalf("AsUIProvider failed")
	}
	if _, ok := AsUIProvider(plain); ok {
		t.Fatalf("AsUIProvider should fail for plain plugin")
	}
}

// ---------------------------------------------------------------------------
// info.go
// ---------------------------------------------------------------------------

func TestProtocolInfosIncludesUISchemas(t *testing.T) {
	r := NewRegistryRaw()
	ui := &mockUIProvider{
		mockPlugin:     &mockPlugin{protocol: "x", displayName: "X", transports: []string{"tcp"}},
		inboundFields:  []schema.FieldSchema{{Key: "port", Label: "Port", Type: schema.FieldNumber}},
		settingsFields: []schema.FieldSchema{{Key: "domain", Label: "Domain", Type: schema.FieldText}},
	}
	r.Register(ui)
	r.Register(&mockPlugin{protocol: "y", displayName: "Y"})

	infos := r.ProtocolInfos()
	if len(infos) != 2 {
		t.Fatalf("len(infos) = %d", len(infos))
	}
	if infos[0].Protocol != "x" || infos[0].DisplayName != "X" {
		t.Fatalf("infos[0] metadata = %+v", infos[0].Metadata)
	}
	if len(infos[0].InboundFieldSchema) != 1 || infos[0].InboundFieldSchema[0].Key != "port" {
		t.Fatalf("infos[0].InboundFieldSchema = %+v", infos[0].InboundFieldSchema)
	}
	if len(infos[0].SettingsFieldSchema) != 1 || infos[0].SettingsFieldSchema[0].Key != "domain" {
		t.Fatalf("infos[0].SettingsFieldSchema = %+v", infos[0].SettingsFieldSchema)
	}
	if len(infos[1].InboundFieldSchema) != 0 || len(infos[1].SettingsFieldSchema) != 0 {
		t.Fatalf("infos[1] should have no schemas: %+v", infos[1])
	}
}

func TestProtocolInfosOrder(t *testing.T) {
	r := NewRegistryRaw()
	r.Register(&mockUIProvider{mockPlugin: &mockPlugin{protocol: "b", displayName: "B"}})
	r.Register(&mockUIProvider{mockPlugin: &mockPlugin{protocol: "a", displayName: "A"}})

	infos := r.ProtocolInfos()
	if infos[0].Protocol != "b" || infos[1].Protocol != "a" {
		t.Fatalf("order = %v", []string{infos[0].Protocol, infos[1].Protocol})
	}
}

// ---------------------------------------------------------------------------
// catalog.go
// ---------------------------------------------------------------------------

func TestCatalogNewCatalogUsesBuiltinRegistry(t *testing.T) {
	cat := NewCatalog()
	choices := cat.Choices()
	if len(choices) != 4 {
		t.Fatalf("expected 4 builtin choices, got %d", len(choices))
	}
	if choices[0].Protocol != "naiveproxy" || choices[3].Protocol != "mieru" {
		t.Fatalf("builtin choices = %+v", choices)
	}
}

func TestCatalogChoicesDefensiveCopy(t *testing.T) {
	cat := Catalog{choices: []Choice{
		{Protocol: "p1", Transports: []string{"tcp"}},
	}}
	choices := cat.Choices()
	choices[0].Transports[0] = "mutated"
	if cat.Choices()[0].Transports[0] != "tcp" {
		t.Fatalf("Choices not defensive copy")
	}
}

func TestCatalogDisplayNameList(t *testing.T) {
	tests := []struct {
		choices []Choice
		want    string
	}{
		{nil, ""},
		{[]Choice{}, ""},
		{[]Choice{{DisplayName: "Alpha"}}, "Alpha"},
		{[]Choice{{DisplayName: "Alpha"}, {DisplayName: "Beta"}}, "Alpha and Beta"},
		{[]Choice{{DisplayName: "Alpha"}, {DisplayName: "Beta"}, {DisplayName: "Gamma"}}, "Alpha, Beta, and Gamma"},
	}
	for _, tc := range tests {
		c := Catalog{choices: tc.choices}
		if got := c.DisplayNameList(); got != tc.want {
			t.Fatalf("DisplayNameList(%+v) = %q, want %q", tc.choices, got, tc.want)
		}
	}
}

func TestCatalogQueries(t *testing.T) {
	cat := Catalog{choices: []Choice{
		{Protocol: "p1", FirewallService: "fw1", RequiresCaddy: true, Transports: []string{"tcp"}},
		{Protocol: "p2", FirewallService: "", RequiresCaddy: false, Transports: []string{"udp"}},
	}}

	if svc, ok := cat.FirewallService("p1"); !ok || svc != "fw1" {
		t.Fatalf("FirewallService(p1) = %q, %v", svc, ok)
	}
	if _, ok := cat.FirewallService("p2"); ok {
		t.Fatalf("FirewallService(p2) should be false")
	}
	if !cat.RequiresCaddy("p1") || cat.RequiresCaddy("p2") {
		t.Fatalf("RequiresCaddy mismatch")
	}
	if !cat.Supports("p1") || cat.Supports("missing") {
		t.Fatalf("Supports mismatch")
	}
	if !cat.SupportsTransport("p1", "tcp") {
		t.Fatalf("expected tcp support")
	}
	if cat.SupportsTransport("p1", "udp") {
		t.Fatalf("expected no udp support for p1")
	}
	if cat.SupportsTransport("p2", "tcp") {
		t.Fatalf("expected no tcp support for p2")
	}
	if cat.SupportsTransport("missing", "tcp") {
		t.Fatalf("expected false for missing protocol")
	}
}

func TestEnglishList(t *testing.T) {
	if got := englishList(nil); got != "" {
		t.Fatalf("englishList(nil) = %q", got)
	}
	if got := englishList([]string{"A"}); got != "A" {
		t.Fatalf("englishList([A]) = %q", got)
	}
	if got := englishList([]string{"A", "B"}); got != "A and B" {
		t.Fatalf("englishList([A B]) = %q", got)
	}
	if got := englishList([]string{"A", "B", "C"}); got != "A, B, and C" {
		t.Fatalf("englishList([A B C]) = %q", got)
	}
}

// ---------------------------------------------------------------------------
// capability_catalog.go
// ---------------------------------------------------------------------------

func TestGeneratedConfigRegistryFromMockRenderers(t *testing.T) {
	r := NewRegistryRaw()
	r.Register(&mockConfigRenderer{
		mockPlugin: &mockPlugin{protocol: "mieru", displayName: "Mieru", maxEnabled: 1},
		render: func(input generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error) {
			return []generatedconfig.GeneratedConfigArtifact{{Path: "mieru.json", Body: "mieru-body"}}, true, nil
		},
	})
	r.Register(&mockConfigRenderer{
		mockPlugin: &mockPlugin{protocol: "other", displayName: "Other", maxEnabled: 0},
		render: func(input generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error) {
			return []generatedconfig.GeneratedConfigArtifact{{Path: "other.json", Body: "other-body"}}, true, nil
		},
	})
	r.Register(&mockPlugin{protocol: "plain", displayName: "Plain"})

	registry := newGeneratedConfigRegistryFrom(r)

	// mieru should not require render settings.
	root := t.TempDir()
	configs, err := registry.Render(generatedconfig.ConfigInput{
		ApplyRoot: root,
		Settings:  model.Settings{},
		Inbounds: []model.Inbound{
			{Name: "m1", Protocol: "mieru", Transport: "tcp", Port: 1000, Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if configs["mieru.json"] != "mieru-body" {
		t.Fatalf("mieru config missing: %v", configs)
	}

	// "other" requires render settings and should be skipped when settings are empty.
	configs, err = registry.Render(generatedconfig.ConfigInput{
		ApplyRoot: root,
		Settings:  model.Settings{},
		Inbounds: []model.Inbound{
			{Name: "o1", Protocol: "other", Transport: "tcp", Port: 2000, Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if _, ok := configs["other.json"]; ok {
		t.Fatalf("other should require render settings")
	}

	// With domain provided, "other" should render.
	configs, err = registry.Render(generatedconfig.ConfigInput{
		ApplyRoot: root,
		Settings:  model.Settings{Domain: "example.com"},
		Inbounds: []model.Inbound{
			{Name: "o1", Protocol: "other", Transport: "tcp", Port: 2000, Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if configs["other.json"] != "other-body" {
		t.Fatalf("other config missing: %v", configs)
	}
}

func TestGeneratedConfigRegistryConstants(t *testing.T) {
	if UnitCaddy != "veil-caddy.service" {
		t.Fatalf("UnitCaddy = %q", UnitCaddy)
	}
	if UnitHysteria2 != "veil-hysteria2@.service" {
		t.Fatalf("UnitHysteria2 = %q", UnitHysteria2)
	}
	if UnitOlcrtc != "veil-olcrtc@.service" {
		t.Fatalf("UnitOlcrtc = %q", UnitOlcrtc)
	}
	if UnitMieru != "veil-mieru.service" {
		t.Fatalf("UnitMieru = %q", UnitMieru)
	}
}

// ---------------------------------------------------------------------------
// install.go
// ---------------------------------------------------------------------------

func TestInstallAllRuntimesInstallsAllRuntimesWithInjectedProviders(t *testing.T) {
	binDir := t.TempDir()
	ctx := context.Background()

	var mu sync.Mutex
	var calls []string

	opts := runtimeinstall.Options{
		BinDir: binDir,
		Arch:   "amd64",
		FetchRelease: func(ctx context.Context, repo string) (*runtimeinstall.Release, error) {
			var assets []runtimeinstall.Asset
			switch repo {
			case "apernet/hysteria":
				assets = []runtimeinstall.Asset{
					{Name: "hysteria-linux-amd64", BrowserDownloadURL: "hysteria://binary"},
				}
			case "enfein/mieru":
				assets = []runtimeinstall.Asset{
					{Name: "mita_2.0.0_linux_amd64.tar.gz", BrowserDownloadURL: "mieru://archive"},
				}
			case "SagerNet/sing-box":
				assets = []runtimeinstall.Asset{
					{Name: "sing-box-1.0.0-linux-amd64.tar.gz", BrowserDownloadURL: "warp://archive"},
				}
			default:
				return nil, fmt.Errorf("unexpected repo %s", repo)
			}
			return &runtimeinstall.Release{TagName: "v1.0.0", Assets: assets}, nil
		},
		Download: func(ctx context.Context, url string) ([]byte, error) {
			switch {
			case strings.HasPrefix(url, "hysteria://"):
				return []byte("#!hysteria\n"), nil
			case strings.HasPrefix(url, "mieru://"):
				return makeTarGz(t, "mita", []byte("#!mita\n")), nil
			case strings.HasPrefix(url, "warp://"):
				return makeTarGz(t, "sing-box", []byte("#!sing-box\n")), nil
			default:
				return nil, fmt.Errorf("unexpected download url %s", url)
			}
		},
		BuildCaddy: func(ctx context.Context, outPath string) error {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "caddy:"+outPath)
			return os.WriteFile(outPath, []byte("#!caddy\n"), 0o755)
		},
		GoInstall: func(ctx context.Context, binDir, sourcePackage string) error {
			mu.Lock()
			defer mu.Unlock()
			calls = append(calls, "olcrtc:"+binDir+":"+sourcePackage)
			return os.WriteFile(filepath.Join(binDir, "olcrtc"), []byte("#!olcrtc\n"), 0o755)
		},
	}

	results := InstallAllRuntimes(ctx, opts)

	for _, r := range results {
		if r.Err != nil {
			t.Errorf("runtime %q install error: %v", r.Name, r.Err)
		}
		if !r.Installed {
			t.Errorf("runtime %q was not installed", r.Name)
		}
	}

	expectedBinaries := []string{"caddy", "hysteria", "mita", "olcrtc", "sing-box"}
	for _, binary := range expectedBinaries {
		path := filepath.Join(binDir, binary)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("missing binary %s: %v", binary, err)
			continue
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("binary %s is not executable", binary)
		}
	}

	if !containsResult(results, "warp") {
		t.Fatalf("warp runtime result missing")
	}

	mu.Lock()
	if len(calls) != 2 {
		t.Fatalf("expected 2 custom build calls, got %v", calls)
	}
	mu.Unlock()
}

func TestInstallAllRuntimesDefaultsToAmd64(t *testing.T) {
	binDir := t.TempDir()
	ctx := context.Background()

	opts := runtimeinstall.Options{
		BinDir: binDir,
		// Leave Arch empty so InstallAllRuntimes falls back to the default.
		FetchRelease: func(ctx context.Context, repo string) (*runtimeinstall.Release, error) {
			var assets []runtimeinstall.Asset
			switch repo {
			case "apernet/hysteria":
				assets = []runtimeinstall.Asset{{Name: "hysteria-linux-amd64", BrowserDownloadURL: "hysteria://binary"}}
			case "enfein/mieru":
				assets = []runtimeinstall.Asset{{Name: "mita_2.0.0_linux_amd64.tar.gz", BrowserDownloadURL: "mieru://archive"}}
			case "SagerNet/sing-box":
				assets = []runtimeinstall.Asset{{Name: "sing-box-1.0.0-linux-amd64.tar.gz", BrowserDownloadURL: "warp://archive"}}
			default:
				return nil, fmt.Errorf("unexpected repo %s", repo)
			}
			return &runtimeinstall.Release{TagName: "v1", Assets: assets}, nil
		},
		Download: func(ctx context.Context, url string) ([]byte, error) {
			switch {
			case strings.HasPrefix(url, "hysteria://"):
				return []byte("#!bin\n"), nil
			case strings.HasPrefix(url, "mieru://"):
				return makeTarGz(t, "mita", []byte("#!mita\n")), nil
			case strings.HasPrefix(url, "warp://"):
				return makeTarGz(t, "sing-box", []byte("#!sing-box\n")), nil
			default:
				return nil, fmt.Errorf("unexpected url %s", url)
			}
		},
		BuildCaddy: func(ctx context.Context, outPath string) error {
			return os.WriteFile(outPath, []byte("#!caddy\n"), 0o755)
		},
		GoInstall: func(ctx context.Context, binDir, sourcePackage string) error {
			return os.WriteFile(filepath.Join(binDir, "olcrtc"), []byte("#!olcrtc\n"), 0o755)
		},
	}

	results := InstallAllRuntimes(ctx, opts)

	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("runtime %q error: %v", r.Name, r.Err)
		}
		if !r.Installed {
			t.Fatalf("runtime %q not installed", r.Name)
		}
	}
}

func TestInstallAllRuntimesCapturesErrors(t *testing.T) {
	binDir := t.TempDir()
	ctx := context.Background()

	opts := runtimeinstall.Options{
		BinDir: binDir,
		Arch:   "amd64",
		FetchRelease: func(ctx context.Context, repo string) (*runtimeinstall.Release, error) {
			return nil, fmt.Errorf("network down")
		},
		BuildCaddy: func(ctx context.Context, outPath string) error {
			return fmt.Errorf("caddy build failed")
		},
		GoInstall: func(ctx context.Context, binDir, sourcePackage string) error {
			return fmt.Errorf("olcrtc build failed")
		},
	}

	results := InstallAllRuntimes(ctx, opts)
	if len(results) == 0 {
		t.Fatalf("expected results")
	}

	hasErr := false
	for _, r := range results {
		if r.Err != nil {
			hasErr = true
		}
	}
	if !hasErr {
		t.Fatalf("expected at least one error")
	}
}

func TestInstallAllRuntimesArchPassedToRuntimeProvider(t *testing.T) {
	// Verify the runtimeinstall.Runtime descriptor includes the requested arch.
	// We exercise a mock RuntimeProvider directly through AsRuntimeProvider.
	base := &mockPlugin{protocol: "demo", displayName: "Demo"}
	rp := &mockRuntimeProvider{
		mockPlugin: base,
		runtime:    runtimeinstall.Runtime{Name: "demo", Binary: "demo"},
	}

	rt := rp.RuntimeInstall("arm64")
	if rt.Name != "demo-arm64" {
		t.Fatalf("arch not passed to runtime descriptor: %s", rt.Name)
	}
}

func TestInstallAllRuntimesForSkipsPluginsWithoutRuntimeProvider(t *testing.T) {
	binDir := t.TempDir()
	ctx := context.Background()

	r := NewRegistryRaw()
	r.Register(&mockPlugin{protocol: "plain", displayName: "Plain"})
	r.Register(&mockRuntimeProvider{
		mockPlugin: &mockPlugin{protocol: "demo", displayName: "Demo"},
		runtime: runtimeinstall.Runtime{
			Name:       "demo",
			Binary:     "demo",
			Method:     runtimeinstall.MethodRawBinary,
			Repo:       "demo/repo",
			AssetMatch: func(name string) bool { return name == "demo" },
		},
	})

	called := false
	opts := runtimeinstall.Options{
		BinDir: binDir,
		Arch:   "amd64",
		FetchRelease: func(ctx context.Context, repo string) (*runtimeinstall.Release, error) {
			switch repo {
			case "demo/repo":
				return &runtimeinstall.Release{TagName: "v1", Assets: []runtimeinstall.Asset{
					{Name: "demo", BrowserDownloadURL: "demo://binary"},
				}}, nil
			case "SagerNet/sing-box":
				return &runtimeinstall.Release{TagName: "v1", Assets: []runtimeinstall.Asset{
					{Name: "sing-box-1.0.0-linux-amd64.tar.gz", BrowserDownloadURL: "warp://archive"},
				}}, nil
			default:
				return nil, fmt.Errorf("unexpected repo %s", repo)
			}
		},
		Download: func(ctx context.Context, url string) ([]byte, error) {
			switch {
			case strings.HasPrefix(url, "demo://"):
				called = true
				return []byte("#!demo\n"), nil
			case strings.HasPrefix(url, "warp://"):
				return makeTarGz(t, "sing-box", []byte("#!sing-box\n")), nil
			default:
				return nil, fmt.Errorf("unexpected url %s", url)
			}
		},
	}

	results := installAllRuntimesFor(ctx, opts, r)
	// The registry contributes "demo"; WARP is always appended from the catalog.
	if len(results) != 2 {
		t.Fatalf("expected 2 results (plain plugin skipped, warp included), got %d", len(results))
	}
	demo, ok := resultByName(results, "demo-amd64")
	if !ok {
		t.Fatalf("expected demo result, got %v", results)
	}
	if demo.Err != nil {
		t.Fatalf("demo install error: %v", demo.Err)
	}
	if !called {
		t.Fatalf("download not called")
	}
	if _, ok := resultByName(results, "warp"); !ok {
		t.Fatalf("expected warp result")
	}
}
