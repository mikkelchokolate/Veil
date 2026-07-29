package generatedconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestRoutingSourceMaterialFetchesVerifiesAndWritesFiles(t *testing.T) {
	root := t.TempDir()
	body := []byte("geoip-body")
	sum := sha256.Sum256(body)
	checksum := []byte(hex.EncodeToString(sum[:]) + " geoip.dat\n")
	file := RoutingSourceFile{Name: "geoip.dat", URL: "https://example.test/geoip.dat", SHA256URL: "https://example.test/geoip.dat.sha256sum", SignatureURL: "https://example.test/geoip.dat.bundle", CertificateIdentity: "test-identity", CertificateOIDCIssuer: "test-issuer"}
	material := NewRoutingSourceMaterial(root, RoutingSource{Files: []RoutingSourceFile{file}}).WithDownloader(func(url string) ([]byte, error) {
		switch url {
		case "https://example.test/geoip.dat":
			return body, nil
		case "https://example.test/geoip.dat.sha256sum":
			return checksum, nil
		case "https://example.test/geoip.dat.bundle":
			return []byte("bundle"), nil
		default:
			t.Fatalf("unexpected download URL %s", url)
			return nil, nil
		}
	}).WithSignatureVerifier(func(context.Context, RoutingSourceFile, []byte, []byte) error { return nil })

	written, err := material.WriteGenerated()
	if err != nil {
		t.Fatalf("WriteGenerated: %v", err)
	}
	path := filepath.Join(root, "generated", "rules", "geoip.dat")
	if len(written) != 1 || written[0] != path {
		t.Fatalf("written = %+v, want %s", written, path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("written body = %q", string(got))
	}
}
