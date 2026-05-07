package api

import "testing"

func TestStatusRoutesIncludesStatusPath(t *testing.T) {
	paths := StatusRoutes{}.Paths()
	if len(paths) != 1 || paths[0] != "/api/status" {
		t.Fatalf("unexpected status route paths: %+v", paths)
	}
}
