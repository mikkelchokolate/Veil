package diagnostics

import (
	"context"
	"net"
	"net/http"
	"os/exec"
	"strings"
)

var dnsLookuper = RunDNSLookup

var pingRunner = RunPing

// execCommandContext is swapped in tests so external commands can be mocked.
var execCommandContext = exec.CommandContext

// lookupHostFunc and lookupCNAMEFunc are swapped in tests to mock DNS resolution.
var lookupHostFunc = net.LookupHost
var lookupCNAMEFunc = net.LookupCNAME

type PingResult struct {
	Host        string  `json:"host"`
	Transmitted int     `json:"transmitted"`
	Received    int     `json:"received"`
	LossPct     float64 `json:"lossPct"`
	MinMs       float64 `json:"minMs,omitempty"`
	AvgMs       float64 `json:"avgMs,omitempty"`
	MaxMs       float64 `json:"maxMs,omitempty"`
	StddevMs    float64 `json:"stddevMs,omitempty"`
	Error       string  `json:"error,omitempty"`
}

type DiagnosticTools struct{}

func (DiagnosticTools) DNSLookup(hostname string) map[string]any {
	addrs, cname, err := dnsLookuper(hostname)
	return NewDNSLookupResult(hostname, addrs, cname, err).Map()
}

func (DiagnosticTools) Ping(host string, count int) PingResult {
	return pingRunner(host, count)
}

func (DiagnosticTools) Speedtest(r *http.Request) (SpeedtestResult, error) {
	return speedtestRunner(r)
}

func RunPing(host string, count int) PingResult {
	result := PingResult{Host: host, Transmitted: count}
	policy := NewPingCommandPolicy()
	ctx, cancel := context.WithTimeout(context.Background(), policy.Timeout(count))
	defer cancel()
	output, err := execCommandContext(ctx, "ping", policy.Args(host, count)...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			result.Error = "ping failed: " + err.Error()
		} else {
			result.Error = "ping failed: " + msg
		}
		result.LossPct = 100
		return result
	}
	parsePingOutput(string(output), &result)
	if result.Transmitted > 0 {
		result.LossPct = float64(result.Transmitted-result.Received) / float64(result.Transmitted) * 100
	}
	return result
}

func RunDNSLookup(host string) ([]string, string, error) {
	addrs, err := lookupHostFunc(host)
	if err != nil {
		return nil, "", err
	}
	cname, _ := lookupCNAMEFunc(host)
	// Only return cname if it differs from the host (i.e. there is a CNAME record)
	if strings.TrimSuffix(cname, ".") == strings.TrimSuffix(host, ".") {
		cname = ""
	}
	return addrs, strings.TrimSuffix(cname, "."), nil
}
