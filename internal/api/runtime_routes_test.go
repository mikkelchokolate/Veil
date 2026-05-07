package api

import "testing"

func TestRuntimeRoutesIncludesRuntimeStatusPaths(t *testing.T) {
	paths := RuntimeRoutes{}.Paths()
	for _, want := range []string{"/api/system", "/api/tls", "/api/network", "/api/connections", "/api/processes", "/api/disk"} {
		if !containsRuntimeRoutePath(paths, want) {
			t.Fatalf("route %s missing from %+v", want, paths)
		}
	}
}

func containsRuntimeRoutePath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
