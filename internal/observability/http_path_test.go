package observability

import "testing"

func TestNormalizeHTTPPathTemplatesSensitiveAndUniqueSegments(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/s/super-secret-token", want: "/s/{token}"},
		{path: "/api/v1/clients/client-1", want: "/api/v1/clients/{id}"},
		{path: "/api/v1/clients/client-2", want: "/api/v1/clients/{id}"},
		{path: "/api/users/alice", want: "/api/users/{username}"},
		{path: "/api/inbounds/hy2-main", want: "/api/inbounds/{name}"},
		{path: "/api/backups/veil_backup_1.tar.gz.enc", want: "/api/backups/{name}"},
		{path: "/api/version/update/jobs/9f3d2c", want: "/api/version/update/jobs/{id}"},
		{path: "/api/status", want: "/api/status"},
		{path: "/probe/unique-aaaa", want: unmatchedHTTPPath},
		{path: "/probe/unique-bbbb", want: unmatchedHTTPPath},
	}
	for _, test := range tests {
		if got := NormalizeHTTPPath(test.path); got != test.want {
			t.Errorf("NormalizeHTTPPath(%q)=%q want %q", test.path, got, test.want)
		}
	}
}

// TestNormalizeHTTPPathPrefersLiteralRouteOverPlaceholder (#1032): when a
// literal route and a {placeholder} template both match, the literal must win.
// The old tie-break preferred the longer pattern string, so
// /api/v1/traffic/{clientId} (27 bytes) shadowed /api/v1/traffic/top (19
// bytes) and the real endpoint's metrics folded into the templated label.
func TestNormalizeHTTPPathPrefersLiteralRouteOverPlaceholder(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/api/v1/traffic/top", want: "/api/v1/traffic/top"},
		{path: "/api/v1/traffic/summary", want: "/api/v1/traffic/summary"},
		{path: "/api/v1/traffic/stream", want: "/api/v1/traffic/stream"},
		{path: "/api/v1/traffic/client-9", want: "/api/v1/traffic/{clientId}"},
		{path: "/api/v1/clients/bulk", want: "/api/v1/clients/bulk"},
		{path: "/api/v1/clients/migrate-legacy", want: "/api/v1/clients/migrate-legacy"},
		{path: "/api/v1/clients/abc", want: "/api/v1/clients/{id}"},
		{path: "/api/apply/state", want: "/api/apply/state"},
		{path: "/api/apply/jobs/job-1", want: "/api/apply/jobs/{id}"},
		{path: "/api/services/sing-box/restart", want: "/api/services/{name}/restart"},
		{path: "/api/services/sing-box/status", want: "/api/services/{name}/{action}"},
		{path: "/api/backups/prune", want: "/api/backups/prune"},
		{path: "/api/backups/snapshot.tar.gz", want: "/api/backups/{name}"},
	}
	for _, test := range tests {
		if got := NormalizeHTTPPath(test.path); got != test.want {
			t.Errorf("NormalizeHTTPPath(%q)=%q want %q", test.path, got, test.want)
		}
	}
}

func TestNormalizeHTTPPathBoundsUnauthenticatedProbes(t *testing.T) {
	first := NormalizeHTTPPath("/no-such/" + "aaaaaaaaaaaaaaaa")
	second := NormalizeHTTPPath("/no-such/" + "bbbbbbbbbbbbbbbb")
	if first != unmatchedHTTPPath || second != unmatchedHTTPPath {
		t.Fatalf("unmatched paths escaped the bound: %q %q", first, second)
	}
}
