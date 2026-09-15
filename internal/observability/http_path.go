package observability

import "strings"

const unmatchedHTTPPath = "/{unmatched}"

var metricPathPatterns = []string{
	"/healthz",
	"/livez",
	"/readyz",
	"/metrics",
	"/s/{token}",
	"/api/auth/login",
	"/api/auth/logout",
	"/api/auth/status",
	"/api/auth/locale",
	"/api/auth/sessions",
	"/api/setup/status",
	"/api/setup/complete",
	"/api/status",
	"/api/version",
	"/api/version/update",
	"/api/health",
	"/api/settings",
	"/api/protocols",
	"/api/protocols/{protocol}/room",
	"/api/inbounds",
	"/api/inbounds/{name}",
	"/api/inbounds/{name}/clients",
	"/api/routing/rules",
	"/api/routing/rules/{name}",
	"/api/routing/presets",
	"/api/routing/presets/{name}",
	"/api/warp",
	"/api/firewall",
	"/api/validation",
	"/api/services/{name}/{action}",
	"/api/services/{name}/restart",
	"/api/apply",
	"/api/apply/plan",
	"/api/apply/reconcile",
	"/api/apply/history",
	"/api/apply/state",
	"/api/apply/jobs",
	"/api/apply/jobs/{id}",
	"/api/apply/jobs/{id}/retry",
	"/api/apply/rollback",
	"/api/tools/dns-lookup",
	"/api/tools/ping",
	"/api/tools/speedtest",
	"/api/profiles/ru-recommended/preview",
	"/api/system",
	"/api/tls",
	"/api/network",
	"/api/connections",
	"/api/processes",
	"/api/disk",
	"/api/runtime/observation",
	"/api/runtime/provenance",
	"/api/v1/clients",
	"/api/v1/clients/bulk",
	"/api/v1/clients/migrate-legacy",
	"/api/v1/clients/{id}",
	"/api/v1/clients/{id}/bindings",
	"/api/v1/clients/{id}/bindings/{bindingId}",
	"/api/v1/clients/{id}/credentials/{bindingId}",
	"/api/v1/clients/{id}/credentials/{bindingId}/rotate",
	"/api/v1/clients/{id}/tokens",
	"/api/v1/clients/{id}/tokens/{tokenId}",
	"/api/v1/clients/{id}/tokens/{tokenId}/rotate",
	"/api/v1/clients/{id}/links",
	"/api/v1/clients/{id}/audit",
	"/api/v1/traffic/top",
	"/api/v1/traffic/summary",
	"/api/v1/traffic/stream",
	"/api/v1/traffic/{clientId}",
	"/api/v1/traffic/{clientId}/history",
	"/api/v1/events",
	"/api/audit",
	"/api/users",
	"/api/users/{username}",
	"/api/backups",
	"/api/backups/prune",
	"/api/backups/{name}",
	"/api/backups/{name}/verify",
	"/api/backups/{name}/download",
	"/api/backups/{name}/restore",
	"/api/backup-restore-jobs/{id}",
	"/api/client-links",
	"/api/client-links/subscription",
	"/api/client-links/qr",
	"/api/logs",
	"/api/admin/rotate-key",
}

// NormalizeHTTPPath maps a request path onto a bounded route template so
// Prometheus path labels cannot include subscription tokens or unique IDs.
func NormalizeHTTPPath(path string) string {
	if path == "" {
		return unmatchedHTTPPath
	}
	best := ""
	bestParts := 0
	for _, pattern := range metricPathPatterns {
		if !matchMetricPathPattern(pattern, path) {
			continue
		}
		parts := splitMetricPath(pattern)
		if len(parts) > bestParts || (len(parts) == bestParts && len(pattern) > len(best)) {
			best = pattern
			bestParts = len(parts)
		}
	}
	if best != "" {
		return best
	}
	return unmatchedHTTPPath
}

func matchMetricPathPattern(pattern, path string) bool {
	patternParts := splitMetricPath(pattern)
	pathParts := splitMetricPath(path)
	if len(patternParts) != len(pathParts) {
		return false
	}
	for i := range patternParts {
		part := patternParts[i]
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			if pathParts[i] == "" {
				return false
			}
			continue
		}
		if part != pathParts[i] {
			return false
		}
	}
	return true
}

func splitMetricPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}
