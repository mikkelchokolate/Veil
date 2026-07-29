package generatedconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRoutingSourceMaterialFetchUsesInjectedDownloader(t *testing.T) {
	called := false
	download := func(url string) ([]byte, error) {
		called = true
		if url == "https://example.test/geoip.dat.sha256sum" {
			sum := sha256.Sum256([]byte("body"))
			return []byte(hex.EncodeToString(sum[:]) + " geoip.dat\n"), nil
		}
		if url == "https://example.test/geoip.dat.bundle" {
			return []byte("bundle"), nil
		}
		return []byte("body"), nil
	}
	file := RoutingSourceFile{Name: "geoip.dat", URL: "https://example.test/geoip.dat", SHA256URL: "https://example.test/geoip.dat.sha256sum", SignatureURL: "https://example.test/geoip.dat.bundle", CertificateIdentity: "test-identity", CertificateOIDCIssuer: "test-issuer"}
	material := NewRoutingSourceMaterial("/etc/veil", RoutingSource{Files: []RoutingSourceFile{file}}).
		WithDownloader(download).
		WithSignatureVerifier(func(context.Context, RoutingSourceFile, []byte, []byte) error { return nil })
	body, err := material.Fetch(material.source.Files[0])
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !called || string(body) != "body" {
		t.Fatalf("default downloader not used: called=%v body=%q", called, string(body))
	}
}

func TestRoutingSourceMaterialFetchReturnsBodyDownloadError(t *testing.T) {
	material := NewRoutingSourceMaterial("/etc/veil", RoutingSource{Files: []RoutingSourceFile{{Name: "geoip.dat", URL: "https://example.test/geoip.dat", SHA256URL: "https://example.test/geoip.dat.sha256sum"}}}).WithDownloader(func(url string) ([]byte, error) {
		if url == "https://example.test/geoip.dat" {
			return nil, errors.New("body download failed")
		}
		return nil, errors.New("unexpected URL")
	})

	_, err := material.Fetch(material.source.Files[0])
	if err == nil || err.Error() != "body download failed" {
		t.Fatalf("expected body download error, got %v", err)
	}
}

func TestRoutingSourceMaterialFetchReturnsChecksumDownloadError(t *testing.T) {
	material := NewRoutingSourceMaterial("/etc/veil", RoutingSource{Files: []RoutingSourceFile{{Name: "geoip.dat", URL: "https://example.test/geoip.dat", SHA256URL: "https://example.test/geoip.dat.sha256sum"}}}).WithDownloader(func(url string) ([]byte, error) {
		switch url {
		case "https://example.test/geoip.dat":
			return []byte("body"), nil
		case "https://example.test/geoip.dat.sha256sum":
			return nil, errors.New("checksum download failed")
		default:
			return nil, errors.New("unexpected URL")
		}
	})

	_, err := material.Fetch(material.source.Files[0])
	if err == nil || err.Error() != "checksum download failed" {
		t.Fatalf("expected checksum download error, got %v", err)
	}
}

func TestRoutingSourceMaterialFetchReturnsChecksumVerifyError(t *testing.T) {
	material := NewRoutingSourceMaterial("/etc/veil", RoutingSource{Files: []RoutingSourceFile{{Name: "geoip.dat", URL: "https://example.test/geoip.dat", SHA256URL: "https://example.test/geoip.dat.sha256sum"}}}).WithDownloader(func(url string) ([]byte, error) {
		switch url {
		case "https://example.test/geoip.dat":
			return []byte("body"), nil
		case "https://example.test/geoip.dat.sha256sum":
			return []byte("badchecksum  geoip.dat\n"), nil
		default:
			return nil, errors.New("unexpected URL")
		}
	})

	_, err := material.Fetch(material.source.Files[0])
	if err == nil {
		t.Fatal("expected checksum verification error")
	}
}

func TestRoutingSourceMaterialWriteGeneratedReturnsFileWriteError(t *testing.T) {
	tmp := t.TempDir()
	badRoot := filepath.Join(tmp, "root")
	if err := os.WriteFile(badRoot, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("create file root: %v", err)
	}

	body := []byte("geoip-body")
	sum := sha256.Sum256(body)
	checksum := []byte(hex.EncodeToString(sum[:]) + " geoip.dat\n")
	material := NewRoutingSourceMaterial(badRoot, RoutingSource{Files: []RoutingSourceFile{{Name: "geoip.dat", URL: "https://example.test/geoip.dat", SHA256URL: "https://example.test/geoip.dat.sha256sum"}}}).WithDownloader(func(url string) ([]byte, error) {
		switch url {
		case "https://example.test/geoip.dat":
			return body, nil
		case "https://example.test/geoip.dat.sha256sum":
			return checksum, nil
		default:
			return nil, errors.New("unexpected URL")
		}
	})

	_, err := material.WriteGenerated()
	if err == nil {
		t.Fatal("expected file write error")
	}
}

func TestRoutingSourceMaterialWriteGeneratedReturnsFetchError(t *testing.T) {
	root := t.TempDir()
	material := NewRoutingSourceMaterial(root, RoutingSource{Files: []RoutingSourceFile{{Name: "geoip.dat", URL: "https://example.test/geoip.dat", SHA256URL: "https://example.test/geoip.dat.sha256sum"}}}).WithDownloader(func(url string) ([]byte, error) {
		return nil, errors.New("fetch failed")
	})

	_, err := material.WriteGenerated()
	if err == nil || err.Error() != "fetch failed" {
		t.Fatalf("expected fetch error, got %v", err)
	}
}
