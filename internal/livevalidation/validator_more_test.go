package livevalidation

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
	"github.com/mikkelchokolate/Veil/internal/service"
)

// fakeUnitInspectorWithError supports returning an error for the
// runtime_unit_missing error-path branch.
type fakeUnitInspectorWithError struct{}

func (fakeUnitInspectorWithError) Exists(context.Context, string) (bool, error) {
	return false, errors.New("unit check failed")
}

// mockProtocolPlugin implements protocols.ProtocolPlugin for test registries.
type mockProtocolPlugin struct {
	protocol   string
	transports []string
}

func (p mockProtocolPlugin) Protocol() string        { return p.protocol }
func (p mockProtocolPlugin) DisplayName() string     { return p.protocol }
func (p mockProtocolPlugin) Transports() []string    { return append([]string(nil), p.transports...) }
func (p mockProtocolPlugin) RequiresCaddy() bool     { return false }
func (p mockProtocolPlugin) FirewallService() string { return "" }
func (p mockProtocolPlugin) MaxEnabled() int         { return 0 }

// mockValidatorPlugin implements Validator but not RuntimeProvider.
type mockValidatorPlugin struct {
	mockProtocolPlugin
}

func (mockValidatorPlugin) ValidateSettings(model.Settings) error { return nil }
func (mockValidatorPlugin) ValidateInbound(model.Settings, model.Inbound) []model.ValidationIssue {
	return nil
}
func (mockValidatorPlugin) NeedsDomain(model.Settings, model.Inbound) bool   { return false }
func (mockValidatorPlugin) HasCredential(model.Settings, model.Inbound) bool { return true }

// mockRuntimeOnlyPlugin implements RuntimeProvider but not Validator.
type mockRuntimeOnlyPlugin struct {
	mockProtocolPlugin
}

func (mockRuntimeOnlyPlugin) RuntimeDescriptors([]model.Inbound) []service.ManagedRuntime { return nil }
func (mockRuntimeOnlyPlugin) RuntimeInstall(string) runtimeinstall.Runtime {
	return runtimeinstall.Runtime{Binary: "mock-binary"}
}

func swapProtocolRegistry(fn func() *protocols.Registry) func() {
	original := protocolRegistry
	protocolRegistry = fn
	return func() { protocolRegistry = original }
}

func TestValidatorSkipsDisabledInbounds(t *testing.T) {
	validator := testValidator()
	validator.Ports = fakePortProbe{err: errors.New("should not be called")}

	response := validator.Validate(context.Background(), Request{
		Inbounds: []model.Inbound{{
			Name: "disabled", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: false, Password: "secret",
		}},
	})

	if !response.Valid {
		t.Fatalf("disabled inbound should not invalidate: %+v", response)
	}
	if len(response.Issues) != 0 {
		t.Fatalf("expected no issues, got %+v", response.Issues)
	}
}

func TestValidatorRejectsEmptyInboundName(t *testing.T) {
	response := testValidator().Validate(context.Background(), Request{
		Inbounds: []model.Inbound{{
			Name: "", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret",
		}},
	})

	assertIssueCode(t, response, "name_required")
}

func TestValidatorRejectsUnsupportedTransportForProtocol(t *testing.T) {
	response := testValidator().Validate(context.Background(), Request{
		Inbounds: []model.Inbound{{
			Name: "edge", Protocol: "mieru", Transport: "sctp", Port: 443, Enabled: true, Password: "secret",
		}},
	})

	assertIssueCode(t, response, "unsupported_transport")
}

func TestValidatorReportsUnitInspectorError(t *testing.T) {
	validator := testValidator()
	validator.Units = fakeUnitInspectorWithError{}

	response := validator.Validate(context.Background(), Request{
		Inbounds: []model.Inbound{{
			Name: "edge", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret",
		}},
	})

	assertIssueCode(t, response, "runtime_unit_missing")
}

func TestValidatorHandlesProtocolWithoutValidator(t *testing.T) {
	defer swapProtocolRegistry(func() *protocols.Registry {
		r := protocols.NewRegistryRaw()
		r.Register(mockRuntimeOnlyPlugin{mockProtocolPlugin{protocol: "novalidator", transports: []string{"tcp"}}})
		return r
	})()

	validator := testValidator()
	validator.Binaries = nil
	validator.Units = nil

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{},
		Inbounds: []model.Inbound{{
			Name: "edge", Protocol: "novalidator", Transport: "tcp", Port: 443, Enabled: true, Password: "secret",
		}},
	})

	if !response.Valid {
		t.Fatalf("protocol without validator should not produce candidate errors: %+v", response)
	}
}

func TestRuntimeIssuesReturnsNilForUnknownProtocol(t *testing.T) {
	issues := (Validator{}).runtimeIssues(context.Background(), model.Inbound{Protocol: "unknown"})
	if len(issues) != 0 {
		t.Fatalf("expected no issues for unknown protocol, got %+v", issues)
	}
}

func TestValidatorHandlesProtocolWithoutRuntimeProvider(t *testing.T) {
	defer swapProtocolRegistry(func() *protocols.Registry {
		r := protocols.NewRegistryRaw()
		r.Register(mockValidatorPlugin{mockProtocolPlugin{protocol: "noruntime", transports: []string{"tcp"}}})
		return r
	})()

	validator := testValidator()

	response := validator.Validate(context.Background(), Request{
		Inbounds: []model.Inbound{{
			Name: "edge", Protocol: "noruntime", Transport: "tcp", Port: 443, Enabled: true, Password: "secret",
		}},
	})

	if !response.Valid {
		t.Fatalf("protocol without runtime provider should not produce live-host warnings: %+v", response)
	}
	for _, issue := range response.Issues {
		if issue.Code == "runtime_binary_missing" || issue.Code == "runtime_unit_missing" {
			t.Fatalf("unexpected runtime issue: %+v", issue)
		}
	}
}

func TestUnitForInboundFallsBackToFirstNonEmptyUnit(t *testing.T) {
	got := unitForInbound("mieru", model.Inbound{Name: "relay"}, []service.ManagedRuntime{
		{Name: "other", Unit: "veil-mieru@other.service"},
	})
	want := "veil-mieru@other.service"
	if got != want {
		t.Fatalf("unit = %q, want %q", got, want)
	}
}

func TestUnitForInboundReturnsExactWhenNoDescriptorsMatch(t *testing.T) {
	got := unitForInbound("mieru", model.Inbound{Name: "relay"}, nil)
	want := "veil-mieru@relay.service"
	if got != want {
		t.Fatalf("unit = %q, want %q", got, want)
	}
}

func TestProtocolNeedsDomainReturnsFalseForUnknownProtocol(t *testing.T) {
	if protocolNeedsDomain(model.Settings{}, model.Inbound{Protocol: "unknown"}) {
		t.Fatal("unknown protocol should not need domain")
	}
}

func TestHasCredentialReturnsTrueForUnknownProtocol(t *testing.T) {
	if !hasCredential(model.Settings{}, model.Inbound{Protocol: "unknown"}) {
		t.Fatal("unknown protocol should report credential present")
	}
}

func TestParseListenPortRejectsOutOfRangePort(t *testing.T) {
	for _, listen := range []string{"127.0.0.1:0", "127.0.0.1:65536"} {
		if got := parseListenPort(listen); got != 0 {
			t.Fatalf("parseListenPort(%q) = %d, want 0", listen, got)
		}
	}
}

func TestDisplayInboundIDReturnsAnotherInboundWhenNameEmpty(t *testing.T) {
	if got := displayInboundID(model.Inbound{Name: ""}); got != "another inbound" {
		t.Fatalf("displayInboundID = %q, want another inbound", got)
	}
}

func TestContainsReturnsFalseForMissingValue(t *testing.T) {
	if contains([]string{"tcp", "udp"}, "sctp") {
		t.Fatal("contains reported sctp in tcp/udp")
	}
}

func TestNormalizeIssueSortKeyReturnsValueWhenMalformed(t *testing.T) {
	for _, value := range []string{"", "a", "a:b", "a:b:c"} {
		if got := normalizeIssueSortKey(value); got != value {
			t.Fatalf("normalizeIssueSortKey(%q) = %q, want %q", value, got, value)
		}
	}
}

func TestSortIssuesIsStableForEqualKeys(t *testing.T) {
	issues := []model.ValidationIssue{
		{Severity: SeverityError, InboundID: "a", Field: "port", Code: "port_invalid"},
		{Severity: SeverityError, InboundID: "a", Field: "port", Code: "port_invalid"},
	}
	sortIssues(issues)
	if !reflect.DeepEqual(issues[0], issues[1]) {
		t.Fatal("sortIssues changed equal-key issues")
	}
}

func TestSeverityRankMapsInfoAndUnknownSeverities(t *testing.T) {
	cases := []struct {
		severity string
		want     string
	}{
		{SeverityInfo, "2"},
		{"unknown", "3"},
	}
	for _, tc := range cases {
		if got := severityRank(tc.severity); got != tc.want {
			t.Fatalf("severityRank(%q) = %q, want %q", tc.severity, got, tc.want)
		}
	}
}
