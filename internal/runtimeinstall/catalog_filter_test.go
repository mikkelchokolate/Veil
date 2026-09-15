package runtimeinstall

import (
	"strings"
	"testing"
)

func TestFilterCatalogSelectsRequestedRuntimes(t *testing.T) {
	catalog := []Runtime{{Name: "mieru"}, {Name: "hysteria2"}, {Name: "warp"}}
	got, err := FilterCatalog(catalog, []string{" Mieru ", "HYSTERIA2", "mieru"})
	if err != nil {
		t.Fatalf("FilterCatalog: %v", err)
	}
	if len(got) != 2 || got[0].Name != "mieru" || got[1].Name != "hysteria2" {
		t.Fatalf("got %+v", got)
	}
}

func TestFilterCatalogEmptyOnlyReturnsAll(t *testing.T) {
	catalog := []Runtime{{Name: "mieru"}, {Name: "warp"}}
	got, err := FilterCatalog(catalog, nil)
	if err != nil {
		t.Fatalf("FilterCatalog: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("empty only should install all, got %+v", got)
	}
}

func TestFilterCatalogRejectsUnknownNamesWithoutPartialMatch(t *testing.T) {
	catalog := []Runtime{{Name: "mieru"}, {Name: "hysteria2"}}
	got, err := FilterCatalog(catalog, []string{"mieru", "hysetria2"})
	if err == nil {
		t.Fatalf("expected unknown-name error, got %+v", got)
	}
	if got != nil {
		t.Fatalf("invalid selection must not return a partial catalog: %+v", got)
	}
	msg := err.Error()
	for _, want := range []string{"hysetria2", "supported", "mieru", "hysteria2"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
}

func TestFilterCatalogRejectsUnknownOnlyAndMultipleUnknowns(t *testing.T) {
	catalog := []Runtime{{Name: "mieru"}}
	_, err := FilterCatalog(catalog, []string{"hysetria2", "nope"})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "hysetria2") || !strings.Contains(msg, "nope") {
		t.Fatalf("error should list every unknown name: %v", err)
	}
}
