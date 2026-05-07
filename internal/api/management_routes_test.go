package api

import "testing"

func TestManagementRoutesIncludesCorePanelManagementPaths(t *testing.T) {
	routes := ManagementRoutes{}
	paths := routes.Paths()
	for _, want := range []string{"/api/settings", "/api/inbounds", "/api/warp", "/api/client-links", "/api/apply"} {
		if !containsRoutePath(paths, want) {
			t.Fatalf("route %s missing from %+v", want, paths)
		}
	}
}

func containsRoutePath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
