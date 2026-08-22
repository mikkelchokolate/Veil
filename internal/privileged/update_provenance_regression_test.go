package privileged

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
)

func TestProductionUpdateRejectsUnsignedStagedReleaseBeforeReplacingBinary(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, updateflow.AssetName())
	archive := makeUnsignedVeilArchive(t, []byte("new-veil-binary"))
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	checksumsPath := filepath.Join(root, "checksums.txt")
	if err := os.WriteFile(checksumsPath, []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(digest[:]), updateflow.AssetName())), 0o600); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(root, "veil")
	if err := os.WriteFile(binaryPath, []byte("old-veil-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := runProductionUpdate(ProductionConfig{BinaryPath: binaryPath}, ResolvedUpdate{
		ArtifactID:    updateflow.AssetName(),
		Version:       "v1.2.4",
		Path:          archivePath,
		ChecksumsPath: checksumsPath,
	})
	if err == nil {
		t.Fatal("API staged update trusted archive/checksums without a verified cosign bundle and SLSA provenance")
	}
	body, readErr := os.ReadFile(binaryPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(body) != "old-veil-binary" {
		t.Fatalf("unsigned update modified live binary: %q", body)
	}
}

func makeUnsignedVeilArchive(t *testing.T, body []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "veil", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
