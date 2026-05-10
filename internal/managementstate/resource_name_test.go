package managementstate

import "testing"

func TestResourceNameParsesSinglePathSegment(t *testing.T) {
	parser := NewResourceNameParser("/api/routing/rules/")
	name, ok := parser.Parse("/api/routing/rules/block-ads")
	if !ok || name != "block-ads" {
		t.Fatalf("name=%q ok=%v", name, ok)
	}
}

func TestResourceNameRejectsEmptyNestedAndWrongPrefix(t *testing.T) {
	parser := NewResourceNameParser("/api/routing/rules/")
	for _, path := range []string{"/api/routing/rules/", "/api/routing/rules/a/b", "/api/routing/presets/ru"} {
		if name, ok := parser.Parse(path); ok {
			t.Fatalf("expected reject for %q, got %q", path, name)
		}
	}
}
