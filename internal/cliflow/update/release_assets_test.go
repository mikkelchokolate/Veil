package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
)

func TestReleaseAssetsDownloadsVerifiedArchive(t *testing.T) {
	assetName := AssetName()
	archive := []byte("archive-body")
	hash := sha256.Sum256(archive)
	checksums := []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(hash[:]), assetName))
	var downloaded []string

	assets := newTestReleaseAssets(assetName, func(url string) ([]byte, error) {
		downloaded = append(downloaded, url)
		switch url {
		case "https://example.com/archive":
			return archive, nil
		case "https://example.com/checksums":
			return checksums, nil
		default:
			return []byte("test-evidence"), nil
		}
	})

	got, err := assets.DownloadVerifiedArchive()
	if err != nil {
		t.Fatalf("DownloadVerifiedArchive: %v", err)
	}
	if got.Name != assetName || string(got.Body) != string(archive) || string(got.Checksums) != string(checksums) {
		t.Fatalf("archive = %+v", got)
	}
	wantDownloads := "[https://example.com/archive https://example.com/checksums https://example.com/checksums.bundle https://example.com/provenance https://example.com/provenance.bundle]"
	if fmt.Sprint(downloaded) != wantDownloads {
		t.Fatalf("downloaded = %v", downloaded)
	}
}
