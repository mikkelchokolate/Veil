package update

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestReleaseAssetsReturnsErrorWhenAssetMissing(t *testing.T) {
	assets := NewReleaseAssets(&Release{TagName: "v1.0.0", Assets: []Asset{
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums"},
	}}, func(string) ([]byte, error) { return nil, errors.New("should not be called") })

	_, err := assets.DownloadVerifiedArchive()
	if err == nil {
		t.Fatal("expected error when asset missing")
	}
}

func TestReleaseAssetsReturnsErrorWhenChecksumsMissing(t *testing.T) {
	assetName := AssetName()
	assets := NewReleaseAssets(&Release{TagName: "v1.0.0", Assets: []Asset{
		{Name: assetName, BrowserDownloadURL: "https://example.com/archive"},
	}}, func(string) ([]byte, error) { return nil, errors.New("should not be called") })

	_, err := assets.DownloadVerifiedArchive()
	if err == nil {
		t.Fatal("expected error when checksums missing")
	}
}

func TestReleaseAssetsReturnsErrorWhenArchiveDownloadFails(t *testing.T) {
	assetName := AssetName()
	assets := newTestReleaseAssets(assetName, func(url string) ([]byte, error) {
		if url == "https://example.com/archive" {
			return nil, errors.New("network down")
		}
		return []byte("checksums"), nil
	})

	_, err := assets.DownloadVerifiedArchive()
	if err == nil {
		t.Fatal("expected error when archive download fails")
	}
}

func TestReleaseAssetsReturnsErrorWhenChecksumsDownloadFails(t *testing.T) {
	assetName := AssetName()
	assets := newTestReleaseAssets(assetName, func(url string) ([]byte, error) {
		if url == "https://example.com/checksums" {
			return nil, errors.New("network down")
		}
		return []byte("archive"), nil
	})

	_, err := assets.DownloadVerifiedArchive()
	if err == nil {
		t.Fatal("expected error when checksums download fails")
	}
}

func TestReleaseAssetsReturnsErrorWhenChecksumVerificationFails(t *testing.T) {
	assetName := AssetName()
	assets := newTestReleaseAssets(assetName, func(url string) ([]byte, error) {
		if url == "https://example.com/archive" {
			return []byte("archive"), nil
		}
		return []byte(fmt.Sprintf("0000000000000000000000000000000000000000000000000000000000000000  %s\n", assetName)), nil
	})

	_, err := assets.DownloadVerifiedArchive()
	if err == nil {
		t.Fatal("expected error when checksum verification fails")
	}
}

func TestAssetNameNormalizesAmd64(t *testing.T) {
	orig := assetNameGOARCH
	assetNameGOARCH = func() string { return "amd64" }
	defer func() { assetNameGOARCH = orig }()
	if got := AssetName(); got != fmt.Sprintf("veil_%s_amd64.tar.gz", runtime.GOOS) {
		t.Fatalf("AssetName = %q", got)
	}
}

func TestAssetNameNormalizesX86_64(t *testing.T) {
	orig := assetNameGOARCH
	assetNameGOARCH = func() string { return "x86_64" }
	defer func() { assetNameGOARCH = orig }()
	if got := AssetName(); got != fmt.Sprintf("veil_%s_amd64.tar.gz", runtime.GOOS) {
		t.Fatalf("AssetName = %q", got)
	}
}

func TestAssetNameNormalizesArm64(t *testing.T) {
	orig := assetNameGOARCH
	assetNameGOARCH = func() string { return "arm64" }
	defer func() { assetNameGOARCH = orig }()
	if got := AssetName(); got != fmt.Sprintf("veil_%s_arm64.tar.gz", runtime.GOOS) {
		t.Fatalf("AssetName = %q", got)
	}
}

func TestAssetNameNormalizesAarch64(t *testing.T) {
	orig := assetNameGOARCH
	assetNameGOARCH = func() string { return "aarch64" }
	defer func() { assetNameGOARCH = orig }()
	if got := AssetName(); got != fmt.Sprintf("veil_%s_arm64.tar.gz", runtime.GOOS) {
		t.Fatalf("AssetName = %q", got)
	}
}

func TestAssetNamePreservesUnknownArch(t *testing.T) {
	orig := assetNameGOARCH
	assetNameGOARCH = func() string { return "riscv64" }
	defer func() { assetNameGOARCH = orig }()
	if got := AssetName(); got != fmt.Sprintf("veil_%s_riscv64.tar.gz", runtime.GOOS) {
		t.Fatalf("AssetName = %q", got)
	}
}

func TestFindAssetURLReturnsEmptyForMissingAsset(t *testing.T) {
	if got := FindAssetURL([]Asset{}, "missing"); got != "" {
		t.Fatalf("FindAssetURL = %q", got)
	}
}

func TestDownloadAssetReturnsErrorOnNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "veil" {
			t.Fatalf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	orig := HTTPClient
	HTTPClient = server.Client()
	defer func() { HTTPClient = orig }()

	_, err := DownloadAsset(server.URL + "/missing")
	if err == nil {
		t.Fatal("expected error for non-2xx")
	}
}

func TestDownloadAssetReturnsBodyOnSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("body"))
	}))
	defer server.Close()

	orig := HTTPClient
	HTTPClient = server.Client()
	defer func() { HTTPClient = orig }()

	body, err := DownloadAsset(server.URL + "/asset")
	if err != nil {
		t.Fatalf("DownloadAsset: %v", err)
	}
	if string(body) != "body" {
		t.Fatalf("body = %q", string(body))
	}
}

func TestVerifyAssetChecksumReturnsErrorWhenMissing(t *testing.T) {
	err := VerifyAssetChecksum([]byte("archive"), "asset.tar.gz", "")
	if err == nil {
		t.Fatal("expected error when checksum missing")
	}
}

func TestVerifyAssetChecksumReturnsErrorOnMismatch(t *testing.T) {
	archive := []byte("archive")
	hash := sha256.Sum256(archive)
	err := VerifyAssetChecksum([]byte("different"), "asset.tar.gz", fmt.Sprintf("%s  asset.tar.gz\n", hex.EncodeToString(hash[:])))
	if err == nil {
		t.Fatal("expected error on mismatch")
	}
}

func TestExtractChecksumForFileHandlesVariousFormats(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"sha256sum two spaces", "abc123  file.tar.gz", "abc123"},
		{"filename at end", "abc123 file.tar.gz", "abc123"},
		{"filename in middle", "other abc123 file.tar.gz extra", "abc123"},
		{"leading/trailing whitespace", "  abc123  file.tar.gz  ", "abc123"},
		{"empty line", "", ""},
		{"wrong file", "abc123 other.tar.gz", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractChecksumForFile(tc.line, "file.tar.gz")
			if got != tc.want {
				t.Fatalf("ExtractChecksumForFile = %q, want %q", got, tc.want)
			}
		})
	}
}
