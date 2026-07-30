package generatedconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRoutingSourceRejectsChecksumOnlyUnauthenticatedPayload(t *testing.T) {
	body := []byte("checksum-only-attacker-controlled")
	sum := sha256.Sum256(body)
	file := RoutingSourceFile{Name: "geoip.dat", URL: "https://example.test/geoip.dat", SHA256URL: "https://example.test/geoip.dat.sha256"}
	material := NewRoutingSourceMaterial(t.TempDir(), RoutingSource{Files: []RoutingSourceFile{file}}).WithDownloader(func(url string) ([]byte, error) {
		if strings.HasSuffix(url, ".sha256") {
			return []byte(hex.EncodeToString(sum[:]) + "  geoip.dat\n"), nil
		}
		return body, nil
	})
	if _, err := material.Fetch(file); err == nil || !strings.Contains(strings.ToLower(err.Error()), "signature") {
		t.Fatalf("checksum-only routing payload was accepted without authenticated signature: %v", err)
	}
}

func TestRoutingSourceRequiresPinnedSignatureIdentity(t *testing.T) {
	body := []byte("signed-routing-data")
	sum := sha256.Sum256(body)
	file := RoutingSourceFile{
		Name: "geoip.dat", URL: "https://example.test/geoip.dat", SHA256URL: "https://example.test/geoip.dat.sha256",
		SignatureURL:          "https://example.test/geoip.dat.bundle",
		CertificateIdentity:   "https://github.com/official/routing/.github/workflows/release.yml@refs/tags/v1",
		CertificateOIDCIssuer: "https://token.actions.githubusercontent.com",
	}
	var verified atomic.Bool

	material := NewRoutingSourceMaterial(t.TempDir(), RoutingSource{Files: []RoutingSourceFile{file}}).WithDownloader(func(url string) ([]byte, error) {
		switch {
		case strings.HasSuffix(url, ".sha256"):
			return []byte(hex.EncodeToString(sum[:]) + "  geoip.dat\n"), nil
		case strings.HasSuffix(url, ".bundle"):
			return []byte("signed-bundle"), nil
		default:
			return body, nil
		}
	}).WithSignatureVerifier(func(context.Context, RoutingSourceFile, []byte, []byte) error {
		verified.Store(true)
		return nil
	})
	got, err := material.WithContext(context.Background()).Fetch(file)
	if err != nil {
		t.Fatalf("authenticated payload rejected: %v", err)
	}
	if string(got) != string(body) || !verified.Load() {
		t.Fatalf("signature verifier not required: verified=%v body=%q", verified.Load(), got)
	}
}

func TestRouteDatChecksumMustNameExactRequestedFile(t *testing.T) {
	body := []byte("routing")
	sum := sha256.Sum256(body)
	manifest := hex.EncodeToString(sum[:]) + "  attacker-other.dat\n"
	if err := verifyRouteDatChecksum("geoip.dat", body, manifest); err == nil {
		t.Fatal("checksum for a different filename was accepted")
	}
}

func TestRoutingSourceRejectsOversizedAuthenticatedPayload(t *testing.T) {
	const maximum = 64 << 20
	body := make([]byte, maximum+1)
	sum := sha256.Sum256(body)
	file := RoutingSourceFile{Name: "geoip.dat", URL: "https://example.test/geoip.dat", SHA256URL: "https://example.test/geoip.dat.sha256", PinnedSHA256: hex.EncodeToString(sum[:])}
	material := NewRoutingSourceMaterial(t.TempDir(), RoutingSource{Files: []RoutingSourceFile{file}}).WithDownloader(func(url string) ([]byte, error) {
		if strings.HasSuffix(url, ".sha256") {
			return []byte(hex.EncodeToString(sum[:]) + "  geoip.dat\n"), nil
		}
		return body, nil
	})
	if _, err := material.Fetch(file); err == nil || !strings.Contains(strings.ToLower(err.Error()), "large") {
		t.Fatalf("oversized routing payload accepted: %v", err)
	}
}

func TestRoutingSourceMultiFileReplacementIsTransactional(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "generated", "rules")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("old-"+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	newGeoIP := []byte("new-geoip")
	newSite := []byte("new-geosite")
	geoSum := sha256.Sum256(newGeoIP)
	files := []RoutingSourceFile{
		{Name: "geoip.dat", URL: "https://example.test/geoip.dat", SHA256URL: "https://example.test/geoip.dat.sha256", SignatureURL: "https://example.test/geoip.dat.bundle", CertificateIdentity: "https://github.com/official/routing/.github/workflows/release.yml@refs/tags/v1", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"},
		{Name: "geosite.dat", URL: "https://example.test/geosite.dat", SHA256URL: "https://example.test/geosite.dat.sha256", SignatureURL: "https://example.test/geosite.dat.bundle", CertificateIdentity: "https://github.com/official/routing/.github/workflows/release.yml@refs/tags/v1", CertificateOIDCIssuer: "https://token.actions.githubusercontent.com"},
	}
	material := NewRoutingSourceMaterial(root, RoutingSource{Files: files}).WithDownloader(func(url string) ([]byte, error) {
		if strings.HasSuffix(url, ".bundle") {
			return []byte("signed-bundle"), nil
		}
		switch url {
		case files[0].URL:
			return newGeoIP, nil
		case files[0].SHA256URL:
			return []byte(hex.EncodeToString(geoSum[:]) + "  geoip.dat\n"), nil
		case files[1].URL:
			return newSite, nil
		case files[1].SHA256URL:
			return []byte(strings.Repeat("0", 64) + "  geosite.dat\n"), nil
		default:
			return nil, errors.New("unexpected URL")
		}
	}).WithSignatureVerifier(func(context.Context, RoutingSourceFile, []byte, []byte) error { return nil })
	if _, err := material.WriteGenerated(); err == nil {
		t.Fatal("invalid second routing file unexpectedly committed")
	}
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		body, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "old-"+name {
			t.Errorf("last-known-good %s replaced during partial transaction: %q", name, body)
		}
	}
}
