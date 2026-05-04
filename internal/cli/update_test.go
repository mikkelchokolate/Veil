package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUpdateAssetNameMatchesPlatform(t *testing.T) {
	name := updateAssetName()
	if !strings.HasSuffix(name, ".tar.gz") {
		t.Fatalf("expected .tar.gz suffix, got: %s", name)
	}
	parts := strings.SplitN(name, "_", 3)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got: %s", name)
	}
	if parts[1] != runtime.GOOS {
		t.Fatalf("os mismatch: %s vs %s", parts[1], runtime.GOOS)
	}
}

func TestFindAssetURL(t *testing.T) {
	assets := []githubAsset{
		{Name: "veil_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/veil.tar.gz"},
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
	}
	if url := findAssetURL(assets, "veil_linux_amd64.tar.gz"); url != "https://example.com/veil.tar.gz" {
		t.Fatalf("bad URL: %s", url)
	}
	if url := findAssetURL(assets, "nonexistent"); url != "" {
		t.Fatalf("expected empty for missing, got: %s", url)
	}
}

func TestExtractChecksumForFile(t *testing.T) {
	checksums := "abc123  veil_linux_amd64.tar.gz\ndef456  checksums.txt\n"
	if got := extractChecksumForFile(checksums, "veil_linux_amd64.tar.gz"); got != "abc123" {
		t.Fatalf("expected abc123, got %s", got)
	}
	if got := extractChecksumForFile(checksums, "checksums.txt"); got != "def456" {
		t.Fatalf("expected def456, got %s", got)
	}
	if got := extractChecksumForFile(checksums, "missing"); got != "" {
		t.Fatalf("expected empty, got %s", got)
	}
}

func TestVerifyAssetChecksum(t *testing.T) {
	data := []byte("test archive")
	hash := sha256.Sum256(data)
	checksums := fmt.Sprintf("%s  veil_linux_amd64.tar.gz\n", hex.EncodeToString(hash[:]))
	if err := verifyAssetChecksum(data, "veil_linux_amd64.tar.gz", checksums); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := verifyAssetChecksum(data, "veil_linux_amd64.tar.gz", "deadbeef  veil_linux_amd64.tar.gz\n"); err == nil {
		t.Fatal("expected error for bad checksum")
	}
	if err := verifyAssetChecksum(data, "missing.tar.gz", checksums); err == nil {
		t.Fatal("expected error for missing entry")
	}
}

func TestExtractVeilBinary(t *testing.T) {
	archive := createTarGz(t, "veil", []byte("fake binary content"))
	bin, err := extractVeilBinary(archive)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	if string(bin) != "fake binary content" {
		t.Fatalf("got %q", bin)
	}

	empty := createTarGz(t, "other", []byte("x"))
	if _, err := extractVeilBinary(empty); err == nil {
		t.Fatal("expected error when veil not in archive")
	}
}

func TestCopyFileData(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFileData(src, dst); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestReplaceBinaryAtomic(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "veil")
	os.WriteFile(dst, []byte("old"), 0o755)
	if err := replaceBinaryAtomic(dst, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "new" {
		t.Fatalf("got %q", got)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".veil-update-") {
			t.Fatalf("temp file not cleaned: %s", e.Name())
		}
	}
}

func TestUpdateCommandRegistered(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"update", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"--yes", "--dry-run", "--force"} {
		if !strings.Contains(got, want) {
			t.Errorf("help missing %q:\n%s", want, got)
		}
	}
}

func createTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{
		Name: name,
		Size: int64(len(content)),
		Mode: 0o644,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()
	return buf.Bytes()
}
