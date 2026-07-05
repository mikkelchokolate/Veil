package livevalidation

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/model"
)

type fakePortProbe struct {
	available map[string]bool
	err       error
}

func (p fakePortProbe) Available(_ context.Context, transport string, port int) (bool, error) {
	if p.err != nil {
		return false, p.err
	}
	available, ok := p.available[bindingKey(transport, port)]
	if !ok {
		return true, nil
	}
	return available, nil
}

type fakeDNSResolver struct {
	hosts map[string][]string
}

func (r fakeDNSResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	addresses, ok := r.hosts[host]
	if !ok {
		return nil, errors.New("host not found")
	}
	return addresses, nil
}

type fakeBinaryLookup struct {
	found map[string]bool
}

func (l fakeBinaryLookup) LookPath(name string) (string, error) {
	if l.found[name] {
		return "/usr/bin/" + name, nil
	}
	return "", errors.New("binary not found")
}

type fakeUnitInspector struct {
	found map[string]bool
}

func (i fakeUnitInspector) Exists(_ context.Context, unit string) (bool, error) {
	return i.found[unit], nil
}

func TestValidatorRejectsDuplicateEnabledBindings(t *testing.T) {
	response := testValidator().Validate(context.Background(), Request{
		Inbounds: []model.Inbound{
			{Name: "first", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "first-secret"},
			{Name: "second", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "second-secret"},
		},
	})

	if response.Valid {
		t.Fatalf("expected duplicate binding to be invalid: %+v", response)
	}
	assertIssueCode(t, response, "duplicate_binding")
}

func TestValidatorAllowsUnchangedBindingOwnedByCandidate(t *testing.T) {
	inbound := model.Inbound{Name: "edge", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}
	validator := testValidator()
	validator.Ports = fakePortProbe{available: map[string]bool{"tcp:443": false}}

	response := validator.Validate(context.Background(), Request{
		Inbounds:        []model.Inbound{inbound},
		CurrentInbounds: []model.Inbound{inbound},
	})

	if !response.Valid {
		t.Fatalf("unchanged owned binding should remain valid: %+v", response)
	}
	if hasIssueCode(response, "port_in_use") {
		t.Fatalf("unchanged owned binding reported busy: %+v", response)
	}
}

func TestValidatorRejectsNewTCPAndUDPBindingsInUse(t *testing.T) {
	validator := testValidator()
	validator.Ports = fakePortProbe{available: map[string]bool{
		"tcp:443":  false,
		"udp:8443": false,
	}}

	response := validator.Validate(context.Background(), Request{
		Inbounds: []model.Inbound{
			{Name: "tcp-edge", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "tcp-secret"},
			{Name: "udp-edge", Protocol: "mieru", Transport: "udp", Port: 8443, Enabled: true, Password: "udp-secret"},
		},
	})

	if response.Valid {
		t.Fatalf("busy ports should be invalid: %+v", response)
	}
	if got := countIssueCode(response, "port_in_use"); got != 2 {
		t.Fatalf("port_in_use count = %d, want 2: %+v", got, response)
	}
}

func TestValidatorReportsMissingDomainEmailCredentialBinaryAndUnit(t *testing.T) {
	validator := testValidator()
	validator.Binaries = fakeBinaryLookup{found: map[string]bool{}}
	validator.Units = fakeUnitInspector{found: map[string]bool{}}

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{},
		Inbounds: []model.Inbound{{
			Name:      "public",
			Protocol:  "naiveproxy",
			Transport: "tcp",
			Port:      443,
			Enabled:   true,
		}},
	})

	for _, code := range []string{
		"domain_required",
		"email_required",
		"credential_required",
		"runtime_binary_missing",
		"runtime_unit_missing",
	} {
		assertIssueCode(t, response, code)
	}
}

func TestValidatorAcceptsNaiveInboundWithPerInboundDomainAndEmail(t *testing.T) {
	validator := testValidator()
	validator.DNS = fakeDNSResolver{hosts: map[string][]string{"p.example.com": {"203.0.113.10"}}}

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{
			DefaultAcmeEmail: "admin@example.com",
			NaiveUsername:    "veil",
			NaivePassword:    "secret",
		},
		Inbounds: []model.Inbound{{
			Name:      "public",
			Protocol:  "naiveproxy",
			Transport: "tcp",
			Port:      443,
			Enabled:   true,
			ProtocolFields: map[string]any{
				"domain": "p.example.com",
			},
		}},
	})

	if !response.Valid {
		t.Fatalf("naive inbound with per-inbound domain/email should be valid: %+v", response)
	}
	for _, code := range []string{"domain_required", "email_required"} {
		if hasIssueCode(response, code) {
			t.Fatalf("unexpected %s issue: %+v", code, response)
		}
	}
}

func TestValidatorReportsUnresolvedDomainAndProbeFailure(t *testing.T) {
	validator := testValidator()
	validator.Ports = fakePortProbe{err: errors.New("permission denied")}

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{
			Domain:           "missing.example",
			DefaultAcmeEmail: "admin@example.com",
			NaiveUsername:    "veil",
			NaivePassword:    "secret",
		},
		Inbounds: []model.Inbound{{
			Name: "public", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true,
		}},
	})

	assertIssueCode(t, response, "dns_unresolved")
	assertIssueCode(t, response, "port_probe_failed")
}

func TestValidatorTreatsExternalDNSAndRuntimeAvailabilityAsWarnings(t *testing.T) {
	validator := testValidator()
	validator.DNS = fakeDNSResolver{hosts: map[string][]string{}}
	validator.Binaries = fakeBinaryLookup{found: map[string]bool{}}
	validator.Units = fakeUnitInspector{found: map[string]bool{}}

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{
			Domain:           "pending.example",
			DefaultAcmeEmail: "admin@example.com",
			NaiveUsername:    "veil",
			NaivePassword:    "secret",
		},
		Inbounds: []model.Inbound{{
			Name: "public", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true,
		}},
	})

	if !response.Valid {
		t.Fatalf("external readiness warnings should not invalidate a candidate: %+v", response)
	}
	for _, code := range []string{"dns_unresolved", "runtime_binary_missing", "runtime_unit_missing"} {
		if severity := issueSeverity(response, code); severity != SeverityWarning {
			t.Fatalf("%s severity = %q, want warning: %+v", code, severity, response)
		}
	}
}

func TestValidatorRejectsPanelPortCollision(t *testing.T) {
	response := testValidator().Validate(context.Background(), Request{
		Settings: model.Settings{PanelListen: "127.0.0.1:2096"},
		Inbounds: []model.Inbound{{
			Name: "edge", Protocol: "mieru", Transport: "tcp", Port: 2096, Enabled: true, Password: "secret",
		}},
	})

	assertIssueCode(t, response, "reserved_panel_port")
}

func TestValidatorAllowsOlcrtcWithoutDomain(t *testing.T) {
	validator := testValidator()
	validator.Units = fakeUnitInspector{found: map[string]bool{"veil-olcrtc@relay.service": true}}

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{},
		Inbounds: []model.Inbound{{
			Name:      "relay",
			Protocol:  "olcrtc",
			Transport: "udp",
			Port:      3478,
			Enabled:   true,
			Password:  "relay-secret",
		}},
	})

	if hasIssueCode(response, "domain_required") {
		t.Fatalf("olcRTC should not require settings.domain: %+v", response)
	}
	if !response.Valid {
		t.Fatalf("olcRTC without domain should be valid: %+v", response)
	}
}

func TestValidatorSortsIssuesDeterministically(t *testing.T) {
	validator := testValidator()
	validator.Binaries = fakeBinaryLookup{found: map[string]bool{}}
	validator.Units = fakeUnitInspector{found: map[string]bool{}}

	response := validator.Validate(context.Background(), Request{
		Inbounds: []model.Inbound{
			{Name: "z", Protocol: "unknown", Transport: "", Port: 0, Enabled: true},
			{Name: "a", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true},
		},
	})

	got := make([]string, 0, len(response.Issues))
	for _, issue := range response.Issues {
		got = append(got, issue.Severity+":"+issue.InboundID+":"+issue.Field+":"+issue.Code)
	}
	want := append([]string(nil), got...)
	sortIssueKeys(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issues are not sorted:\n got: %v\nwant: %v", got, want)
	}
	if response.CheckedAt != time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC) {
		t.Fatalf("checkedAt = %s", response.CheckedAt)
	}
}

func testValidator() Validator {
	return Validator{
		Ports:    fakePortProbe{available: map[string]bool{}},
		DNS:      fakeDNSResolver{hosts: map[string][]string{"vpn.example.com": {"203.0.113.10"}}},
		Binaries: fakeBinaryLookup{found: map[string]bool{"caddy": true, "hysteria": true, "olcrtc": true, "mieru": true}},
		Units: fakeUnitInspector{found: map[string]bool{
			"veil-caddy@public.service": true,
			"veil-mieru.service":        true,
		}},
		Now: func() time.Time {
			return time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		},
	}
}

func assertIssueCode(t *testing.T, response Response, code string) {
	t.Helper()
	if !hasIssueCode(response, code) {
		t.Fatalf("missing issue %q in %+v", code, response)
	}
}

func hasIssueCode(response Response, code string) bool {
	return countIssueCode(response, code) > 0
}

func countIssueCode(response Response, code string) int {
	count := 0
	for _, issue := range response.Issues {
		if issue.Code == code {
			count++
		}
	}
	return count
}

func issueSeverity(response Response, code string) string {
	for _, issue := range response.Issues {
		if issue.Code == code {
			return issue.Severity
		}
	}
	return ""
}
