package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestBuiltInRoutingSourcePinsImmutableReleaseAssets(t *testing.T) {
	source := routeDatSource()
	if !strings.HasSuffix(source.Repository, "/releases/tag/"+routingRulesRelease) {
		t.Fatalf("repository provenance = %q, want exact release tag", source.Repository)
	}
	if len(source.Files) != 2 {
		t.Fatalf("routing source files = %d, want 2", len(source.Files))
	}
	for _, file := range source.Files {
		if !strings.Contains(file.URL, "/releases/download/"+routingRulesRelease+"/") || strings.Contains(file.URL, "/latest/") {
			t.Errorf("%s URL is not immutable: %q", file.Name, file.URL)
		}
		if !strings.Contains(file.SHA256URL, "/releases/download/"+routingRulesRelease+"/") || strings.Contains(file.SHA256URL, "/latest/") {
			t.Errorf("%s checksum URL is not immutable: %q", file.Name, file.SHA256URL)
		}
		decoded, err := hex.DecodeString(file.PinnedSHA256)
		if err != nil || len(decoded) != sha256.Size || file.PinnedSHA256 != strings.ToLower(file.PinnedSHA256) {
			t.Errorf("%s pinned digest is invalid: %q", file.Name, file.PinnedSHA256)
		}
	}
}
