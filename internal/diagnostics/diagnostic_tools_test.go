package diagnostics

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

var errDiagnosticTest = errors.New("lookup failed")

func TestDiagnosticToolsDNSLookupReturnsEmptyAddressesOnError(t *testing.T) {
	old := dnsLookuper
	dnsLookuper = func(host string) ([]string, string, error) { return nil, "", errDiagnosticTest }
	t.Cleanup(func() { dnsLookuper = old })

	result := DiagnosticTools{}.DNSLookup("example.com")
	if result["hostname"] != "example.com" || result["error"] == "" {
		t.Fatalf("unexpected DNS result: %+v", result)
	}
	addresses, ok := result["addresses"].([]string)
	if !ok || len(addresses) != 0 {
		t.Fatalf("expected empty addresses, got %+v", result["addresses"])
	}
}

func TestDiagnosticToolsDNSLookupReturnsSuccessResult(t *testing.T) {
	old := dnsLookuper
	dnsLookuper = func(host string) ([]string, string, error) {
		return []string{"203.0.113.1", "2001:db8::1"}, "canonical.example.com.", nil
	}
	t.Cleanup(func() { dnsLookuper = old })

	result := DiagnosticTools{}.DNSLookup("example.com")
	if result["hostname"] != "example.com" {
		t.Fatalf("hostname = %+v", result["hostname"])
	}
	if result["error"] != nil {
		t.Fatalf("unexpected error: %+v", result["error"])
	}
	addresses, ok := result["addresses"].([]string)
	if !ok || len(addresses) != 2 || addresses[0] != "203.0.113.1" || addresses[1] != "2001:db8::1" {
		t.Fatalf("addresses = %+v", result["addresses"])
	}
	if result["cname"] != "canonical.example.com." {
		t.Fatalf("cname = %+v", result["cname"])
	}
}

func TestDiagnosticToolsPingReturnsPingRunnerResult(t *testing.T) {
	want := PingResult{Host: "example.com", Transmitted: 3, Received: 3, AvgMs: 12.3}
	old := pingRunner
	pingRunner = func(host string, count int) PingResult {
		if host != "example.com" || count != 3 {
			t.Fatalf("pingRunner called with host=%q count=%d", host, count)
		}
		return want
	}
	t.Cleanup(func() { pingRunner = old })

	got := DiagnosticTools{}.Ping("example.com", 3)
	if got != want {
		t.Fatalf("result = %+v, want %+v", got, want)
	}
}

func TestDiagnosticToolsSpeedtestReturnsSpeedtestRunnerResult(t *testing.T) {
	want := SpeedtestResult{Server: "ISP - Node", PingMS: 10.5, DownloadMbps: 100, UploadMbps: 50}
	old := speedtestRunner
	speedtestRunner = func(r *http.Request) (SpeedtestResult, error) {
		if r == nil {
			t.Fatal("expected non-nil request")
		}
		return want, nil
	}
	t.Cleanup(func() { speedtestRunner = old })

	req, err := http.NewRequest("GET", "/speedtest", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	got, err := DiagnosticTools{}.Speedtest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("result = %+v, want %+v", got, want)
	}
}

func TestDiagnosticToolsSpeedtestPropagatesError(t *testing.T) {
	old := speedtestRunner
	speedtestRunner = func(r *http.Request) (SpeedtestResult, error) {
		return SpeedtestResult{}, errDiagnosticTest
	}
	t.Cleanup(func() { speedtestRunner = old })

	req, err := http.NewRequest("GET", "/speedtest", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	_, err = DiagnosticTools{}.Speedtest(req)
	if !errors.Is(err, errDiagnosticTest) {
		t.Fatalf("error = %v, want %v", err, errDiagnosticTest)
	}
}

func TestRunPingParsesOutputAndCalculatesLoss(t *testing.T) {
	mockExecCommandContext(t, "ping_success")

	got := RunPing("127.0.0.1", 5)
	if got.Host != "127.0.0.1" {
		t.Fatalf("host = %q, want 127.0.0.1", got.Host)
	}
	if got.Transmitted != 5 || got.Received != 3 {
		t.Fatalf("transmitted = %d, received = %d, want 5/3", got.Transmitted, got.Received)
	}
	if got.LossPct != 40 {
		t.Fatalf("lossPct = %v, want 40", got.LossPct)
	}
	if got.MinMs != 1.2 || got.AvgMs != 3.4 || got.MaxMs != 5.6 || got.StddevMs != 0.1 {
		t.Fatalf("timing = %+v, want 1.2/3.4/5.6/0.1", got)
	}
	if got.Error != "" {
		t.Fatalf("unexpected error: %q", got.Error)
	}
}

func TestRunPingReturnsErrorAndFullLossOnFailure(t *testing.T) {
	mockExecCommandContext(t, "ping_failure")

	got := RunPing("badhost", 3)
	if got.Error != "ping failed: ping: unknown host" {
		t.Fatalf("error = %q, want ping failed: ping: unknown host", got.Error)
	}
	if got.LossPct != 100 {
		t.Fatalf("lossPct = %v, want 100", got.LossPct)
	}
}

func TestRunPingFallsBackToErrorTextWhenOutputEmpty(t *testing.T) {
	mockExecCommandContext(t, "ping_failure_nooutput")

	got := RunPing("badhost", 3)
	if !strings.Contains(got.Error, "exit status 1") {
		t.Fatalf("error = %q, want exit status 1", got.Error)
	}
	if got.LossPct != 100 {
		t.Fatalf("lossPct = %v, want 100", got.LossPct)
	}
}

func TestRunPingSkipsLossCalculationWhenNoTransmissions(t *testing.T) {
	mockExecCommandContext(t, "ping_zero_transmitted")

	got := RunPing("127.0.0.1", 0)
	if got.Transmitted != 0 || got.Received != 0 {
		t.Fatalf("transmitted = %d, received = %d, want 0/0", got.Transmitted, got.Received)
	}
	if got.LossPct != 0 {
		t.Fatalf("lossPct = %v, want 0", got.LossPct)
	}
}

func TestRunDNSLookupReturnsAddressesAndCNAME(t *testing.T) {
	oldHost := lookupHostFunc
	oldCNAME := lookupCNAMEFunc
	lookupHostFunc = func(host string) ([]string, error) {
		return []string{"203.0.113.1", "2001:db8::1"}, nil
	}
	lookupCNAMEFunc = func(host string) (string, error) {
		return "canonical.example.com.", nil
	}
	t.Cleanup(func() {
		lookupHostFunc = oldHost
		lookupCNAMEFunc = oldCNAME
	})

	addrs, cname, err := RunDNSLookup("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(addrs) != 2 || addrs[0] != "203.0.113.1" || addrs[1] != "2001:db8::1" {
		t.Fatalf("addrs = %v", addrs)
	}
	if cname != "canonical.example.com" {
		t.Fatalf("cname = %q, want canonical.example.com", cname)
	}
}

func TestRunDNSLookupDropsCNAMEWhenSameAsHost(t *testing.T) {
	oldHost := lookupHostFunc
	oldCNAME := lookupCNAMEFunc
	lookupHostFunc = func(host string) ([]string, error) {
		return []string{"127.0.0.1"}, nil
	}
	lookupCNAMEFunc = func(host string) (string, error) {
		return "localhost.", nil
	}
	t.Cleanup(func() {
		lookupHostFunc = oldHost
		lookupCNAMEFunc = oldCNAME
	})

	_, cname, err := RunDNSLookup("localhost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cname != "" {
		t.Fatalf("cname = %q, want empty", cname)
	}
}

func TestRunDNSLookupReturnsErrorOnLookupFailure(t *testing.T) {
	oldHost := lookupHostFunc
	lookupHostFunc = func(host string) ([]string, error) {
		return nil, errDiagnosticTest
	}
	t.Cleanup(func() { lookupHostFunc = oldHost })

	addrs, cname, err := RunDNSLookup("bad.example")
	if !errors.Is(err, errDiagnosticTest) {
		t.Fatalf("error = %v, want %v", err, errDiagnosticTest)
	}
	if addrs != nil {
		t.Fatalf("addrs = %v, want nil", addrs)
	}
	if cname != "" {
		t.Fatalf("cname = %q, want empty", cname)
	}
}
