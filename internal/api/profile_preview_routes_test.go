package api

import (
	"reflect"
	"testing"
)

func TestRURecommendedPreviewRequestDoesNotExposeStack(t *testing.T) {
	if _, ok := reflect.TypeOf(RURecommendedPreviewRequest{}).FieldByName("Stack"); ok {
		t.Fatalf("RURecommendedPreviewRequest should not expose removed stack field; protocol choices are Panel Inbounds")
	}
}

func TestProfilePreviewRoutesIncludesRURecommendedPreviewPath(t *testing.T) {
	paths := ProfilePreviewRoutes{}.Paths()
	if len(paths) != 1 || paths[0] != "/api/profiles/ru-recommended/preview" {
		t.Fatalf("unexpected profile preview route paths: %+v", paths)
	}
}
