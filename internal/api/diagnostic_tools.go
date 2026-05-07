package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(count+2)*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ping", "-c", strconv.Itoa(count), "-W", "2", host).CombinedOutput()
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

func parsePingOutput(output string, result *PingResult) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "packets transmitted") {
			// e.g. "3 packets transmitted, 3 received, 0% packet loss"
			fmt.Sscanf(line, "%d packets transmitted, %d received", &result.Transmitted, &result.Received)
		}
		if strings.Contains(line, "min/avg/max") || strings.Contains(line, "rtt min/avg/max") {
			// e.g. "rtt min/avg/max/mdev = 1.234/2.345/4.567/0.890 ms"
			parts := strings.Split(line, "=")
			if len(parts) == 2 {
				stats := strings.Fields(strings.TrimSpace(parts[1]))
				if len(stats) >= 1 {
					times := strings.Split(stats[0], "/")
					if len(times) >= 4 {
						fmt.Sscanf(times[0], "%f", &result.MinMs)
						fmt.Sscanf(times[1], "%f", &result.AvgMs)
						fmt.Sscanf(times[2], "%f", &result.MaxMs)
						fmt.Sscanf(times[3], "%f", &result.StddevMs)
					}
				}
			}
		}
	}
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
