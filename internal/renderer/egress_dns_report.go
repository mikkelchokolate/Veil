package renderer

import "os"

// EgressDNSReport describes how the host resolver fares under the kernel
// egress filter of the protocol units (Hysteria2, Mieru, WARP, olcRTC). It
// backs the `veil doctor` DNS check (#1288): those units stay active while
// every lookup is dropped, so without a check the outage is silent.
type EgressDNSReport struct {
	// Nameservers are the resolv.conf nameservers the resolvers query (the
	// first three parseable lines), as written.
	Nameservers []string `json:"nameservers"`
	// Reachable is true when at least one of them is the pierced
	// systemd-resolved stub or a public address. Meaningful only when
	// ReadError is empty.
	Reachable bool `json:"reachable"`
	// ResolvConfMissing is true when /etc/resolv.conf does not exist; the
	// resolvers then fall back to localhost, which the filter denies.
	ResolvConfMissing bool `json:"resolvConfMissing,omitempty"`
	// ReadError is set when /etc/resolv.conf exists but cannot be read. The
	// renderers then keep the host resolver, and the check cannot tell.
	ReadError string `json:"readError,omitempty"`
	// PinnedDoH is the Cloudflare DoH address the Hysteria2 and WARP
	// renderers pin when no nameserver is reachable. Empty otherwise.
	PinnedDoH string `json:"pinnedDoH,omitempty"`
}

// ProbeEgressDNS reads /etc/resolv.conf and the routing tables with the same
// rules the Hysteria2 and WARP renderers use (filteredHostDoHAddr), so the
// report matches what the next apply renders. Unlike the render-time probe it
// is not disabled inside test binaries: callers ask for it explicitly.
func ProbeEgressDNS() EgressDNSReport {
	var report EgressDNSReport
	conf, err := egressHostReadResolvConf()
	switch {
	case err != nil && os.IsNotExist(err):
		report.ResolvConfMissing = true
	case err != nil:
		report.ReadError = err.Error()
		return report
	}
	for _, addr := range resolvConfNameservers(conf) {
		report.Nameservers = append(report.Nameservers, addr.String())
		if nameserverAllowedByEgressFilter(addr) {
			report.Reachable = true
		}
	}
	if !report.Reachable {
		report.PinnedDoH = pickFilteredHostDoHAddr()
	}
	return report
}
