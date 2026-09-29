package api

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/caddycert"
	"github.com/mikkelchokolate/Veil/internal/model"
	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
	"gopkg.in/yaml.v3"
)

// InboundTLSStatus reports the TLS certificate status a single domain-bearing
// inbound actually serves — the certificate resolved from the live hysteria2
// YAML or Caddy storage, never a guessed/claimed source. Source is one of
// "acme" (CA-issued), "internal" (Caddy local CA), "self-signed" (Veil or
// operator fallback material), or "missing" — an internal/fallback
// certificate is never reported as trusted (#1168).
type InboundTLSStatus struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Domain   string `json:"domain"`
	// Pending marks an ACME issuance the cert-sync worker is still retrying
	// after apply — a reported self-signed certificate is provisional until
	// the retries converge or exhaust (#1168).
	Pending bool                    `json:"pending,omitempty"`
	Cert    veilruntime.TLSCertInfo `json:"cert"`
}

// handleInboundTLS serves GET /api/tls/inbounds — the per-inbound certificate
// status view. The read is filesystem-only: it never triggers a privileged
// sync and never mutates state (#1168).
func (r RuntimeRoutes) handleInboundTLS(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if req.Method != http.MethodGet {
		return
	}
	writeJSON(w, r.inboundTLSStatuses())
}

func (r RuntimeRoutes) inboundTLSStatuses() []InboundTLSStatus {
	s := r.State
	if s == nil {
		return []InboundTLSStatus{}
	}
	s.mu.Lock()
	inbounds := make([]Inbound, len(s.inbounds))
	copy(inbounds, s.inbounds)
	settings := s.settings
	liveRoot := s.liveRoot
	worker := s.certSyncWorker
	s.mu.Unlock()

	out := make([]InboundTLSStatus, 0, len(inbounds))
	for _, inbound := range inbounds {
		switch inbound.Protocol {
		case "hysteria2":
			out = append(out, hysteria2InboundTLSStatus(worker, liveRoot, settings, inbound))
		case "naiveproxy":
			if status, ok := caddyServedInboundTLSStatus(settings, inbound); ok {
				out = append(out, status)
			}
		}
	}
	return out
}

// hysteria2InboundTLSStatus resolves the certificate the live hysteria2 YAML
// for inbound points at. The Caddy certificate directory is authoritative for
// ISSUANCE metadata only — the served bytes come from the configured path and
// are classified by content so a seeded Veil fallback reports "self-signed",
// not "acme" (#1168).
func hysteria2InboundTLSStatus(worker *certSyncWorker, liveRoot string, settings Settings, inbound Inbound) InboundTLSStatus {
	status := InboundTLSStatus{Name: inbound.Name, Protocol: inbound.Protocol, Domain: model.ResolveInboundDomain(inbound, settings)}
	if status.Domain != "" {
		status.Pending = worker.PendingDomain(status.Domain)
	}
	certPath, keyPath, err := liveHysteria2TLSPaths(liveRoot, inbound.Name)
	if err != nil {
		status.Cert = veilruntime.TLSCertInfo{
			Source: veilruntime.TLSCertSourceMissing,
			Error:  fmt.Sprintf("no live hysteria2 TLS config for inbound %s: %v", inbound.Name, err),
		}
		return status
	}
	info := veilruntime.ReadManagedTLSCertForDomain(certPath, status.Domain)
	// A readable certificate whose key file is missing or unreadable leaves
	// the runtime unable to start — surface that instead of a green cert.
	if info.Error == "" && keyPath != "" {
		if _, err := os.Stat(keyPath); err != nil {
			info.Valid = false
			info.Error = fmt.Sprintf("key file unavailable: %v", err)
		}
	}
	// When the served bytes are exactly the pair Caddy storage holds, enrich
	// the status with the issuing-storage metadata (issuer name/kind) so an
	// ACME cert shows its real issuer and a Caddy-local cert stays visibly
	// internal. A byte mismatch means the inbound is serving something else —
	// in that case the content classification already told the truth (#1168).
	if status.Domain != "" && info.Source != veilruntime.TLSCertSourceMissing {
		if pair, err := findCaddyTLSCertPair("", status.Domain); err == nil && len(pair.CertPEM) > 0 {
			if served, readErr := os.ReadFile(certPath); readErr == nil && bytes.Equal(served, pair.CertPEM) {
				info.ManagedBy = "caddy"
				info.IssuerSource = pair.IssuerName
				info.IssuerKind = caddycert.IssuerKind(pair.IssuerName)
			}
		}
	}
	status.Cert = info
	return status
}

// caddyServedInboundTLSStatus reports the certificate Caddy serves directly
// for a naive inbound's domain — for TCP/TLS-terminated protocols the live
// config IS Caddy, so the managed pair in Caddy storage is the served
// certificate (#1168). Returns false for non-domain inbounds.
func caddyServedInboundTLSStatus(settings Settings, inbound Inbound) (InboundTLSStatus, bool) {
	domain := model.ResolveInboundDomain(inbound, settings)
	if domain == "" {
		return InboundTLSStatus{}, false
	}
	status := InboundTLSStatus{Name: inbound.Name, Protocol: inbound.Protocol, Domain: domain}
	pair, err := findCaddyTLSCertPair("", domain)
	if err != nil {
		status.Cert = veilruntime.TLSCertInfo{
			Source: veilruntime.TLSCertSourceMissing,
			Error:  fmt.Sprintf("no Caddy-managed certificate for %s: %v", domain, err),
		}
		return status, true
	}
	info := veilruntime.ReadManagedTLSCertForDomain(pair.CertPath, domain)
	info.ManagedBy = "caddy"
	info.IssuerSource = pair.IssuerName
	info.IssuerKind = caddycert.IssuerKind(pair.IssuerName)
	status.Cert = info
	return status, true
}

// liveHysteria2TLSPaths reads the tls.cert/tls.key paths out of the live
// hysteria2 YAML for name — the file the running instance actually serves
// (#1168).
func liveHysteria2TLSPaths(liveRoot, name string) (certPath, keyPath string, err error) {
	if strings.TrimSpace(liveRoot) == "" {
		return "", "", fmt.Errorf("no live config root")
	}
	path := filepath.Join(liveRoot, "hysteria2", name+".yaml")
	body, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	var doc struct {
		TLS struct {
			Cert string `yaml:"cert"`
			Key  string `yaml:"key"`
		} `yaml:"tls"`
	}
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return "", "", fmt.Errorf("parse %s: %w", path, err)
	}
	certPath = strings.TrimSpace(doc.TLS.Cert)
	if certPath == "" {
		return "", "", fmt.Errorf("live hysteria2 config %s has no tls.cert", path)
	}
	return certPath, strings.TrimSpace(doc.TLS.Key), nil
}
