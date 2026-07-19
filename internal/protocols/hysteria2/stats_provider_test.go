package hysteria2

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestStatsProviderRead(t *testing.T) {
	dir := t.TempDir()
	statsPath := filepath.Join(dir, "stats.json")

	// Write stats file.
	entries := []map[string]any{
		{"user": "alice", "upload_bytes": 1024, "download_bytes": 2048},
		{"user": "bob", "upload_bytes": 512, "download_bytes": 256},
		{"user": "unknown", "upload_bytes": 100, "download_bytes": 100}, // not in bindings
	}
	data, _ := json.Marshal(entries)
	if err := os.WriteFile(statsPath, data, 0644); err != nil {
		t.Fatalf("write stats: %v", err)
	}

	bindings := map[string]string{
		"alice": "binding-1",
		"bob":   "binding-2",
	}
	p := NewStatsProvider("test", statsPath, bindings)

	readings, err := p.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(readings) != 2 {
		t.Fatalf("expected 2 readings (unknown skipped), got %d", len(readings))
	}

	if r, ok := readings["binding-1"]; !ok {
		t.Error("missing binding-1 (alice)")
	} else {
		if r.UploadBytes != 1024 || r.DownloadBytes != 2048 {
			t.Errorf("alice counters: up=%d down=%d, want 1024/2048", r.UploadBytes, r.DownloadBytes)
		}
	}

	if r, ok := readings["binding-2"]; !ok {
		t.Error("missing binding-2 (bob)")
	} else {
		if r.UploadBytes != 512 || r.DownloadBytes != 256 {
			t.Errorf("bob counters: up=%d down=%d, want 512/256", r.UploadBytes, r.DownloadBytes)
		}
	}
}

func TestStatsProviderReadMissingFile(t *testing.T) {
	p := NewStatsProvider("test", filepath.Join(t.TempDir(), "nonexistent.json"), nil)
	readings, err := p.Read()
	if err != nil {
		t.Fatalf("Read missing file should not error: %v", err)
	}
	if len(readings) != 0 {
		t.Errorf("expected empty readings for missing file, got %d", len(readings))
	}
}

func TestStatsProviderReadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	statsPath := filepath.Join(dir, "stats.json")
	if err := os.WriteFile(statsPath, []byte("not json"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	p := NewStatsProvider("test", statsPath, nil)
	_, err := p.Read()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestStatsFilePath(t *testing.T) {
	got := StatsFilePath("/var/lib/veil", "hy2-main")
	want := "/var/lib/veil/hysteria2/hy2-main/stats.json"
	if got != want {
		t.Errorf("StatsFilePath = %q, want %q", got, want)
	}
	// Sanitization.
	got = StatsFilePath("/root", "hy2/main")
	want = "/root/hysteria2/hy2_main/stats.json"
	if got != want {
		t.Errorf("StatsFilePath sanitize = %q, want %q", got, want)
	}
}

// Verify interface compliance.
var _ client.TrafficProvider = (*StatsProvider)(nil)
