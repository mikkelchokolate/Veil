package generatedconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRoutingSourceMaterialFetchUsesDefaultDownloader(t *testing.T) {
	old := routeDatDownloader
	t.Cleanup(func() { routeDatDownloader = old })
	called := false
	routeDatDownloader = func(url string) ([]byte, error) {
		called = true
		return []byte("body"), nil
	}

	material := RoutingSourceMaterial{applyRoot: "/etc/veil", source: RoutingSource{Files: []RoutingSourceFile{{Name: "geoip.dat", URL: "https://example.test/geoip.dat"}}}}
	body, err := material.Fetch(material.source.Files[0])
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !called || string(body) != "body" {
		t.Fatalf("default downloader not used: called=%v body=%q", called, string(body))
	}
}

func TestRoutingSourceMaterialFetchReturnsBodyDownloadError(t *testing.T) {
	material := NewRoutingSourceMaterial("/etc/veil", RoutingSource{Files: []RoutingSourceFile{{Name: "geoip.dat", URL: "u", SHA256URL: "s"}}}).WithDownloader(func(url string) ([]byte, error) {
		if url == "u" {
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
	material := NewRoutingSourceMaterial("/etc/veil", RoutingSource{Files: []RoutingSourceFile{{Name: "geoip.dat", URL: "u", SHA256URL: "s"}}}).WithDownloader(func(url string) ([]byte, error) {
		switch url {
		case "u":
			return []byte("body"), nil
		case "s":
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
	material := NewRoutingSourceMaterial("/etc/veil", RoutingSource{Files: []RoutingSourceFile{{Name: "geoip.dat", URL: "u", SHA256URL: "s"}}}).WithDownloader(func(url string) ([]byte, error) {
		switch url {
		case "u":
			return []byte("body"), nil
		case "s":
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
	material := NewRoutingSourceMaterial(badRoot, RoutingSource{Files: []RoutingSourceFile{{Name: "geoip.dat", URL: "u", SHA256URL: "s"}}}).WithDownloader(func(url string) ([]byte, error) {
		switch url {
		case "u":
			return body, nil
		case "s":
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
	material := NewRoutingSourceMaterial(root, RoutingSource{Files: []RoutingSourceFile{{Name: "geoip.dat", URL: "u"}}}).WithDownloader(func(url string) ([]byte, error) {
		return nil, errors.New("fetch failed")
	})

	_, err := material.WriteGenerated()
	if err == nil || err.Error() != "fetch failed" {
		t.Fatalf("expected fetch error, got %v", err)
	}
}
