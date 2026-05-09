package api

import (
	"net/http"

	"github.com/veil-panel/veil/internal/diagnostics"
)

type PingResult = diagnostics.PingResult
type SpeedtestResult = diagnostics.SpeedtestResult

type DiagnosticTools struct{}

var dnsLookuper = diagnostics.RunDNSLookup
var pingRunner = diagnostics.RunPing
var speedtestRunner = diagnostics.RunSpeedtest
var errSpeedtestUnavailable = diagnostics.ErrSpeedtestUnavailable

func (DiagnosticTools) DNSLookup(hostname string) map[string]any {
	addrs, cname, err := dnsLookuper(hostname)
	return diagnostics.NewDNSLookupResult(hostname, addrs, cname, err).Map()
}

func (DiagnosticTools) Ping(host string, count int) PingResult {
	return pingRunner(host, count)
}

func (DiagnosticTools) Speedtest(r *http.Request) (SpeedtestResult, error) {
	return speedtestRunner(r)
}

func NewDNSLookupResult(hostname string, addrs []string, cname string, err error) diagnostics.DNSLookupResult {
	return diagnostics.NewDNSLookupResult(hostname, addrs, cname, err)
}

func NewPingCommandPolicy() diagnostics.PingCommandPolicy { return diagnostics.NewPingCommandPolicy() }

func NewSpeedtestServerLabel(provider, location string) diagnostics.SpeedtestServerLabel {
	return diagnostics.NewSpeedtestServerLabel(provider, location)
}

func parsePingOutput(output string, result *PingResult) { diagnostics.ParsePingOutput(output, result) }
