package api

import "testing"

func TestPanelRoutesIncludesPanelHealthAndVersionPaths(t *testing.T) {
	paths := PanelRoutes{}.Paths()
	for _, want := range []string{"/", "/healthz", "/api/version"} {
		if !containsPanelRoutePath(paths, want) {
			t.Fatalf("route %s missing from %+v", want, paths)
		}
	}
}

func containsPanelRoutePath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
