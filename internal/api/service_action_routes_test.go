package api

import "testing"

func TestServiceActionRoutesIncludesRestartPrefix(t *testing.T) {
	paths := ServiceActionRoutes{}.Paths()
	if len(paths) != 1 || paths[0] != "/api/services/" {
		t.Fatalf("unexpected service action route paths: %+v", paths)
	}
}
