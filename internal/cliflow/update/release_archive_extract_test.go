package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

func TestUpdateReleaseArchiveExtractsVeilBinary(t *testing.T) {
	archive := NewReleaseArchive(createTestTarGz(t, "./veil", []byte("binary-body")))

	binary, err := archive.ExtractVeilBinary()
	if err != nil {
		t.Fatalf("ExtractVeilBinary: %v", err)
	}
	if string(binary) != "binary-body" {
		t.Fatalf("binary = %q", string(binary))
	}
}

func TestUpdateReleaseArchiveExtractsVeilBinaryBareName(t *testing.T) {
	archive := NewReleaseArchive(createTestTarGz(t, "veil", []byte("binary-body")))

	binary, err := archive.ExtractVeilBinary()
	if err != nil {
		t.Fatalf("ExtractVeilBinary: %v", err)
	}
	if string(binary) != "binary-body" {
		t.Fatalf("binary = %q", string(binary))
	}
}

func TestUpdateReleaseArchiveReturnsErrorForInvalidGzip(t *testing.T) {
	archive := NewReleaseArchive([]byte("not gzip"))
	_, err := archive.ExtractVeilBinary()
	if err == nil {
		t.Fatal("expected error for invalid gzip")
	}
}

func TestUpdateReleaseArchiveReturnsErrorForCorruptTar(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte("truncated")); err != nil {
		t.Fatal(err)
	}
	gz.Close()

	archive := NewReleaseArchive(buf.Bytes())
	_, err := archive.ExtractVeilBinary()
	if err == nil {
		t.Fatal("expected error for corrupt tar")
	}
}

// #1212: a "veil" member larger than the 100 MiB cap must error instead of
// being silently truncated by the LimitReader and installed half-written.
func TestUpdateReleaseArchiveRejectsOversizedBinary(t *testing.T) {
	const maxBinSize = 100 * 1024 * 1024
	// Zero bytes keep the gzip layer small while the declared member size
	// exceeds the cap.
	archive := NewReleaseArchive(createTestTarGz(t, "veil", make([]byte, maxBinSize+1)))

	binary, err := archive.ExtractVeilBinary()
	if err == nil {
		t.Fatal("expected error for oversized veil binary")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error should explain the size cap: %v", err)
	}
	if binary != nil {
		t.Fatalf("no binary bytes may be returned on rejection, got %d", len(binary))
	}
}

func TestUpdateReleaseArchiveReturnsErrorWhenBinaryMissing(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "other", Mode: 0o644, Size: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	archive := NewReleaseArchive(buf.Bytes())
	_, err := archive.ExtractVeilBinary()
	if err == nil {
		t.Fatal("expected error when veil binary not found")
	}
}
