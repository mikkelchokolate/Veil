package api

import "testing"

func TestDiagnosticToolRoutesIncludesDNSPingAndSpeedtestPaths(t *testing.T) {
	paths := DiagnosticToolRoutes{}.Paths()
	for _, want := range []string{"/api/tools/dns-lookup", "/api/tools/ping", "/api/tools/speedtest"} {
		if !containsDiagnosticToolRoutePath(paths, want) {
			t.Fatalf("route %s missing from %+v", want, paths)
		}
	}
}

func containsDiagnosticToolRoutePath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
