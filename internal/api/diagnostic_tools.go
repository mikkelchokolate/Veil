package api

import "net/http"

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
