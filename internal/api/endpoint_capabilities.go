package api

import (
	"net/http"
	"strings"
)

type endpointCapability string

const (
	capabilityPublic        endpointCapability = "public"
	capabilityViewer        endpointCapability = "viewer"
	capabilityAdminMetadata endpointCapability = "admin-metadata"
	capabilityAdminSecret   endpointCapability = "admin-secret"
	capabilityAdminMutation endpointCapability = "admin-mutation"
	capabilitySelfService   endpointCapability = "self-service"
)

type endpointPolicy struct {
	method     string
	pattern    string
	capability endpointCapability
}

// endpointPolicies is the authorization source of truth. A route is never
// classified by HTTP method alone and unknown API operations fail closed.
var endpointPolicies = []endpointPolicy{
	{http.MethodGet, "/healthz", capabilityPublic},
	{http.MethodGet, "/metrics", capabilityPublic},
	{http.MethodGet, "/s/{token}", capabilityPublic},
	{http.MethodPost, "/api/auth/login", capabilityPublic},
	{http.MethodPost, "/api/auth/logout", capabilityPublic},
	{http.MethodGet, "/api/auth/status", capabilityPublic},
	{http.MethodGet, "/api/setup/status", capabilityPublic},
	{http.MethodPost, "/api/setup/complete", capabilityPublic},
	{http.MethodPost, "/api/auth/locale", capabilitySelfService},

	{http.MethodGet, "/api/status", capabilityViewer},
	{http.MethodGet, "/api/version", capabilityViewer},
	{http.MethodGet, "/api/settings", capabilityViewer},
	{http.MethodGet, "/api/protocols", capabilityViewer},
	{http.MethodGet, "/api/inbounds", capabilityViewer},
	{http.MethodGet, "/api/inbounds/{name}", capabilityViewer},
	{http.MethodGet, "/api/routing/rules", capabilityViewer},
	{http.MethodGet, "/api/routing/rules/{name}", capabilityViewer},
	{http.MethodGet, "/api/routing/presets", capabilityViewer},
	{http.MethodGet, "/api/warp", capabilityViewer},
	{http.MethodGet, "/api/firewall", capabilityViewer},
	{http.MethodPost, "/api/validation", capabilityViewer},
	{http.MethodPost, "/api/apply/plan", capabilityViewer},
	{http.MethodGet, "/api/apply/history", capabilityViewer},
	{http.MethodGet, "/api/apply/state", capabilityViewer},
	{http.MethodGet, "/api/apply/jobs", capabilityViewer},
	{http.MethodGet, "/api/apply/jobs/{id}", capabilityViewer},
	{http.MethodPost, "/api/tools/dns-lookup", capabilityViewer},
	{http.MethodPost, "/api/tools/ping", capabilityViewer},
	{http.MethodPost, "/api/tools/speedtest", capabilityViewer},
	{http.MethodPost, "/api/profiles/ru-recommended/preview", capabilityViewer},
	{http.MethodGet, "/api/system", capabilityViewer},
	{http.MethodGet, "/api/tls", capabilityViewer},
	{http.MethodGet, "/api/network", capabilityViewer},
	{http.MethodGet, "/api/connections", capabilityViewer},
	{http.MethodGet, "/api/processes", capabilityViewer},
	{http.MethodGet, "/api/disk", capabilityViewer},
	{http.MethodGet, "/api/runtime/observation", capabilityViewer},
	{http.MethodGet, "/api/v1/clients", capabilityViewer},
	{http.MethodGet, "/api/v1/clients/{id}", capabilityViewer},
	{http.MethodGet, "/api/v1/clients/{id}/bindings", capabilityViewer},
	{http.MethodGet, "/api/v1/traffic/top", capabilityViewer},
	{http.MethodGet, "/api/v1/traffic/summary", capabilityViewer},
	{http.MethodGet, "/api/v1/traffic/{clientId}", capabilityViewer},
	{http.MethodGet, "/api/v1/traffic/{clientId}/history", capabilityViewer},
	{http.MethodGet, "/api/v1/traffic/stream", capabilityViewer},
	{http.MethodGet, "/api/v1/events", capabilityViewer},

	{http.MethodGet, "/api/audit", capabilityAdminMetadata},
	{http.MethodGet, "/api/auth/sessions", capabilityAdminMetadata},
	{http.MethodGet, "/api/users", capabilityAdminMetadata},
	{http.MethodGet, "/api/users/{username}", capabilityAdminMetadata},
	{http.MethodGet, "/api/backups", capabilityAdminMetadata},
	{http.MethodGet, "/api/backups/{name}/verify", capabilityAdminMetadata},
	{http.MethodGet, "/api/backup-restore-jobs/{id}", capabilityAdminMetadata},
	{http.MethodGet, "/api/v1/clients/{id}/tokens", capabilityAdminMetadata},
	{http.MethodGet, "/api/v1/clients/{id}/audit", capabilityAdminMetadata},

	{http.MethodGet, "/api/client-links", capabilityAdminSecret},
	{http.MethodGet, "/api/client-links/subscription", capabilityAdminSecret},
	{http.MethodPost, "/api/client-links/qr", capabilityAdminSecret},
	{http.MethodGet, "/api/logs", capabilityAdminSecret},
	{http.MethodGet, "/api/backups/{name}/download", capabilityAdminSecret},
	{http.MethodPost, "/api/v1/clients/{id}/credentials/{bindingId}", capabilityAdminSecret},
	{http.MethodPost, "/api/v1/clients/{id}/credentials/{bindingId}/rotate", capabilityAdminSecret},
	{http.MethodPost, "/api/v1/clients/{id}/tokens", capabilityAdminSecret},
	{http.MethodPost, "/api/v1/clients/{id}/tokens/{tokenId}/rotate", capabilityAdminSecret},
	{http.MethodPost, "/api/{protocol}/room", capabilityAdminSecret},

	{http.MethodPut, "/api/settings", capabilityAdminMutation},
	{http.MethodPost, "/api/inbounds", capabilityAdminMutation},
	{http.MethodPut, "/api/inbounds/{name}", capabilityAdminMutation},
	{http.MethodDelete, "/api/inbounds/{name}", capabilityAdminMutation},
	{http.MethodPost, "/api/routing/rules", capabilityAdminMutation},
	{http.MethodPut, "/api/routing/rules/{name}", capabilityAdminMutation},
	{http.MethodDelete, "/api/routing/rules/{name}", capabilityAdminMutation},
	{http.MethodPost, "/api/routing/presets/{name}", capabilityAdminMutation},
	{http.MethodPut, "/api/warp", capabilityAdminMutation},
	{http.MethodPost, "/api/apply", capabilityAdminMutation},
	{http.MethodPost, "/api/apply/reconcile", capabilityAdminMutation},
	{http.MethodPost, "/api/apply/jobs/{id}/retry", capabilityAdminMutation},
	{http.MethodPost, "/api/apply/rollback", capabilityAdminMutation},
	{http.MethodPost, "/api/services/{name}/restart", capabilityAdminMutation},
	{http.MethodPost, "/api/version/update", capabilityAdminMutation},
	{http.MethodPost, "/api/admin/rotate-key", capabilityAdminMutation},
	{http.MethodDelete, "/api/auth/sessions", capabilityAdminMutation},
	{http.MethodPost, "/api/users", capabilityAdminMutation},
	{http.MethodPut, "/api/users/{username}", capabilityAdminMutation},
	{http.MethodDelete, "/api/users/{username}", capabilityAdminMutation},
	{http.MethodPost, "/api/backups", capabilityAdminMutation},
	{http.MethodPost, "/api/backups/prune", capabilityAdminMutation},
	{http.MethodPost, "/api/backups/{name}/restore", capabilityAdminMutation},
	{http.MethodPost, "/api/backups/{name}/verify", capabilityAdminMutation},
	{http.MethodPost, "/api/v1/clients", capabilityAdminMutation},
	{http.MethodPost, "/api/v1/clients/bulk", capabilityAdminMutation},
	{http.MethodPost, "/api/v1/clients/migrate-legacy", capabilityAdminMutation},
	{http.MethodPatch, "/api/v1/clients/{id}", capabilityAdminMutation},
	{http.MethodDelete, "/api/v1/clients/{id}", capabilityAdminMutation},
	{http.MethodPost, "/api/v1/clients/{id}/bindings", capabilityAdminMutation},
	{http.MethodPatch, "/api/v1/clients/{id}/bindings/{bindingId}", capabilityAdminMutation},
	{http.MethodDelete, "/api/v1/clients/{id}/bindings/{bindingId}", capabilityAdminMutation},
	{http.MethodDelete, "/api/v1/clients/{id}/tokens/{tokenId}", capabilityAdminMutation},
}

func capabilityForEndpoint(method, path string) (endpointCapability, bool) {
	if method == http.MethodOptions {
		return capabilityPublic, true
	}
	if method == http.MethodHead {
		method = http.MethodGet
	}
	for _, policy := range endpointPolicies {
		if policy.method == method && matchEndpointPattern(policy.pattern, path) {
			return policy.capability, true
		}
	}
	// A path that is explicitly registered but invoked with an unsupported
	// method still passes through a conservative admin gate so the handler can
	// return its precise 405/Allow response. It never inherits another method's
	// read capability.
	for _, policy := range endpointPolicies {
		if matchEndpointPattern(policy.pattern, path) {
			return capabilityAdminMutation, true
		}
	}
	// Static assets and SPA routes are public; API and subscription namespaces
	// are only reachable when explicitly listed above.
	if path != "/api" && path != "/s" && !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/s/") {
		return capabilityPublic, true
	}
	return "", false
}

func matchEndpointPattern(pattern, path string) bool {
	patternParts := splitEndpointPath(pattern)
	pathParts := splitEndpointPath(path)
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

func splitEndpointPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func capabilityAllowsRole(capability endpointCapability, role string) bool {
	if capability == capabilityPublic {
		return true
	}
	if role == "admin" {
		return true
	}
	return role == "viewer" && (capability == capabilityViewer || capability == capabilitySelfService)
}
