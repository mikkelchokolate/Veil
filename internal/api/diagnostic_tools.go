package api

import (
	"context"
	"net"
	"net/http"
	"os/exec"
	"strings"
)

var dnsLookuper = runDNSLookup

var pingRunner = runPing

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
	result := map[string]any{
		"hostname":  hostname,
		"addresses": addrs,
	}
	if cname != "" {
		result["cname"] = cname
	}
	if err != nil {
		result["error"] = err.Error()
	}
	if addrs == nil {
		result["addresses"] = []string{}
	}
	return result
}

func (DiagnosticTools) Ping(host string, count int) PingResult {
	return pingRunner(host, count)
}

func (DiagnosticTools) Speedtest(r *http.Request) (SpeedtestResult, error) {
	return speedtestRunner(r)
}

func runPing(host string, count int) PingResult {
	result := PingResult{Host: host, Transmitted: count}
	policy := NewPingCommandPolicy()
	ctx, cancel := context.WithTimeout(context.Background(), policy.Timeout(count))
	defer cancel()
	output, err := exec.CommandContext(ctx, "ping", policy.Args(host, count)...).CombinedOutput()
	if err != nil {
		result.Error = "ping failed: " + strings.TrimSpace(string(output))
		if result.Error == "ping failed:" {
			result.Error = "ping failed: " + err.Error()
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

func runDNSLookup(host string) ([]string, string, error) {
	addrs, err := net.LookupHost(host)
	if err != nil {
		return nil, "", err
	}
	cname, _ := net.LookupCNAME(host)
	// Only return cname if it differs from the host (i.e. there is a CNAME record)
	if strings.TrimSuffix(cname, ".") == strings.TrimSuffix(host, ".") {
		cname = ""
	}
	return addrs, strings.TrimSuffix(cname, "."), nil
}
