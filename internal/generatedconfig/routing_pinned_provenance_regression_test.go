package generatedconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestRoutingSourceMaterialAcceptsExactPinnedDigestWithoutSignatureBundle(t *testing.T) {
	body := []byte("pinned-routing-data")
	digest := sha256.Sum256(body)
	pinned := hex.EncodeToString(digest[:])
	file := RoutingSourceFile{
		Name:         "geoip.dat",
		URL:          "https://example.test/releases/download/v1/geoip.dat",
		SHA256URL:    "https://example.test/releases/download/v1/geoip.dat.sha256sum",
		PinnedSHA256: pinned,
	}
	material := NewRoutingSourceMaterial(t.TempDir(), RoutingSource{Files: []RoutingSourceFile{file}}).
		WithDownloader(func(rawURL string) ([]byte, error) {
			if strings.HasSuffix(rawURL, ".sha256sum") {
				return []byte(pinned + "  geoip.dat\n"), nil
			}
			return body, nil
		})
	got, err := material.Fetch(file)
	if err != nil {
		t.Fatalf("fetch pinned source: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("body = %q, want %q", got, body)
	}
}

func TestRoutingSourceMaterialRejectsPinnedDigestMismatch(t *testing.T) {
	body := []byte("tampered-routing-data")
	expected := sha256.Sum256([]byte("trusted-routing-data"))
	pinned := hex.EncodeToString(expected[:])
	file := RoutingSourceFile{
		Name:         "geoip.dat",
		URL:          "https://example.test/releases/download/v1/geoip.dat",
		SHA256URL:    "https://example.test/releases/download/v1/geoip.dat.sha256sum",
		PinnedSHA256: pinned,
	}
	actual := sha256.Sum256(body)
	material := NewRoutingSourceMaterial(t.TempDir(), RoutingSource{Files: []RoutingSourceFile{file}}).
		WithDownloader(func(rawURL string) ([]byte, error) {
			if strings.HasSuffix(rawURL, ".sha256sum") {
				return []byte(hex.EncodeToString(actual[:]) + "  geoip.dat\n"), nil
			}
			return body, nil
		})
	if _, err := material.Fetch(file); err == nil || !strings.Contains(err.Error(), "pinned SHA-256") {
		t.Fatalf("Fetch error = %v, want pinned digest rejection", err)
	}
}

func TestRoutingSourceMaterialRejectsPartialSignatureMetadataEvenWithPin(t *testing.T) {
	digest := sha256.Sum256([]byte("body"))
	file := RoutingSourceFile{
		Name:                "geoip.dat",
		URL:                 "https://example.test/geoip.dat",
		SHA256URL:           "https://example.test/geoip.dat.sha256sum",
		PinnedSHA256:        hex.EncodeToString(digest[:]),
		CertificateIdentity: "identity-without-bundle-or-issuer",
	}
	if _, err := NewRoutingSourceMaterial(t.TempDir(), RoutingSource{}).Fetch(file); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("Fetch error = %v, want incomplete signature metadata rejection", err)
	}
}
