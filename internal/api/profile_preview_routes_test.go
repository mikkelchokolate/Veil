package api

import "testing"

func TestProfilePreviewRoutesIncludesRURecommendedPreviewPath(t *testing.T) {
	paths := ProfilePreviewRoutes{}.Paths()
	if len(paths) != 1 || paths[0] != "/api/profiles/ru-recommended/preview" {
		t.Fatalf("unexpected profile preview route paths: %+v", paths)
	}
}
