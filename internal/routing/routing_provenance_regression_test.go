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

// TestBuiltInRoutingSourcePinsExactDigests locks the pinned SHA-256 values
// themselves: a shape-only check greens a silent swap to a different
// release's digests, which is exactly the provenance regression #836 guards.
// The values are the published runetfreedom 202607301129 asset digests.
func TestBuiltInRoutingSourcePinsExactDigests(t *testing.T) {
	want := map[string]string{
		"geoip.dat":   "3aeb1cc31bbf0e490217bb2d14a1d207f372bfe92abff1b1c1a01ee59a2f2327",
		"geosite.dat": "ae2b3e8375a00992a979d09c4bc28f14f15f39096349881d3b3c50ae3d1e269a",
	}
	for _, file := range routeDatSource().Files {
		wantDigest, ok := want[file.Name]
		if !ok {
			t.Fatalf("unexpected routing source file %q", file.Name)
		}
		if file.PinnedSHA256 != wantDigest {
			t.Errorf("%s pinned SHA-256 = %q, want %q", file.Name, file.PinnedSHA256, wantDigest)
		}
		wantURL := "https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/download/202607301129/" + file.Name
		if file.URL != wantURL {
			t.Errorf("%s URL = %q, want %q", file.Name, file.URL, wantURL)
		}
	}
}
