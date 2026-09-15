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

func TestNormalizeHTTPPathBoundsUnauthenticatedProbes(t *testing.T) {
	first := NormalizeHTTPPath("/no-such/" + "aaaaaaaaaaaaaaaa")
	second := NormalizeHTTPPath("/no-such/" + "bbbbbbbbbbbbbbbb")
	if first != unmatchedHTTPPath || second != unmatchedHTTPPath {
		t.Fatalf("unmatched paths escaped the bound: %q %q", first, second)
	}
}
