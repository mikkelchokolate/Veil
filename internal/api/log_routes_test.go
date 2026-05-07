package api

import "testing"

func TestLogRoutesIncludesLogsPath(t *testing.T) {
	paths := LogRoutes{}.Paths()
	if len(paths) != 1 || paths[0] != "/api/logs" {
		t.Fatalf("unexpected log route paths: %+v", paths)
	}
}
