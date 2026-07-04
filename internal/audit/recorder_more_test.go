package audit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewRecorderDefaults(t *testing.T) {
	r := NewRecorder("x", RecorderOptions{MaxBytes: -1, Backups: -5})
	if r.maxBytes != defaultRecorderMaxBytes {
		t.Fatalf("maxBytes: got %d, want %d", r.maxBytes, defaultRecorderMaxBytes)
	}
	if r.backups != 0 {
		t.Fatalf("backups: got %d, want 0", r.backups)
	}
	if r.now == nil {
		t.Fatal("now should not be nil")
	}
}

func TestRecorderAppendNilOrEmptyPath(t *testing.T) {
	var r *Recorder
	if err := r.Append(Record{Action: "x"}); err != nil {
		t.Fatalf("nil recorder: %v", err)
	}
	r = NewRecorder("", RecorderOptions{})
	if err := r.Append(Record{Action: "x"}); err != nil {
		t.Fatalf("empty path: %v", err)
	}
}

func TestRecorderAppendTimestampUTC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panel.jsonl")
	recorder := NewRecorder(path, RecorderOptions{
		Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	loc := time.FixedZone("TEST", 2*60*60)
	if err := recorder.Append(Record{
		Actor:     "alice",
		Action:    "ts.test",
		Success:   true,
		Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, loc),
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"timestamp":"2026-01-01T10:00:00Z"`) {
		t.Fatalf("expected UTC timestamp, got %s", body)
	}
}

func TestRecorderAppendMarshalError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.jsonl")
	recorder := NewRecorder(path, RecorderOptions{})
	if err := recorder.Append(Record{
		Action:  "bad",
		Success: true,
		Details: map[string]any{"ch": make(chan int)},
	}); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestRecorderAppendErrors(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*testing.T) (string, func())
		wantErr bool
	}{
		{
			name: "mkdir fails when parent is a file",
			setup: func(t *testing.T) (string, func()) {
				dir := t.TempDir()
				block := filepath.Join(dir, "block")
				if err := os.WriteFile(block, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(block, "panel.jsonl"), func() {}
			},
			wantErr: true,
		},
		{
			name: "rotate stat fails on symlink loop",
			setup: func(t *testing.T) (string, func()) {
				dir := t.TempDir()
				loop := filepath.Join(dir, "loop")
				if err := os.Symlink("loop", loop); err != nil {
					t.Fatal(err)
				}
				return loop, func() {}
			},
			wantErr: true,
		},
		{
			name: "open fails when path is a directory",
			setup: func(t *testing.T) (string, func()) {
				dir := t.TempDir()
				block := filepath.Join(dir, "blockdir")
				if err := os.Mkdir(block, 0o700); err != nil {
					t.Fatal(err)
				}
				return block, func() {}
			},
			wantErr: true,
		},
		{
			name: "write fails on /dev/full",
			setup: func(t *testing.T) (string, func()) {
				if runtime.GOOS != "linux" {
					t.Skip("/dev/full is Linux-specific")
				}
				return "/dev/full", func() {}
			},
			wantErr: true,
		},
		{
			name: "sync fails",
			setup: func(t *testing.T) (string, func()) {
				orig := fileSync
				fileSync = func(*os.File) error { return errors.New("sync fail") }
				return filepath.Join(t.TempDir(), "panel.jsonl"), func() { fileSync = orig }
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, cleanup := tc.setup(t)
			defer cleanup()

			recorder := NewRecorder(path, RecorderOptions{MaxBytes: defaultRecorderMaxBytes})
			err := recorder.Append(Record{Actor: "a", Action: "err.test", Success: true})
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRecorderListEdgeCases(t *testing.T) {
	t.Run("nil recorder", func(t *testing.T) {
		var r *Recorder
		records, err := r.List(10, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != 0 {
			t.Fatalf("expected 0, got %d", len(records))
		}
	})
	t.Run("empty path", func(t *testing.T) {
		r := NewRecorder("", RecorderOptions{})
		records, err := r.List(10, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != 0 {
			t.Fatalf("expected 0, got %d", len(records))
		}
	})
	t.Run("limit defaults and caps", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "panel.jsonl")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 505; i++ {
			rec := Record{
				Timestamp: time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC),
				Actor:     "a",
				Action:    "cap",
				Target:    strconv.Itoa(i),
				Success:   true,
			}
			b, _ := json.Marshal(rec)
			f.Write(b)
			f.WriteString("\n")
		}
		f.Close()

		r := NewRecorder(path, RecorderOptions{})
		records, err := r.List(0, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != 100 {
			t.Fatalf("limit<=0: expected 100, got %d", len(records))
		}

		records, err = r.List(1000, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != 500 {
			t.Fatalf("limit>500: expected 500, got %d", len(records))
		}
		if records[0].Target != "504" || records[499].Target != "5" {
			t.Fatalf("capped ordering wrong: first=%s last=%s", records[0].Target, records[499].Target)
		}
	})
	t.Run("read records error", func(t *testing.T) {
		dir := t.TempDir()
		loop := filepath.Join(dir, "loop")
		if err := os.Symlink("loop", loop); err != nil {
			t.Fatal(err)
		}
		r := NewRecorder(loop, RecorderOptions{})
		_, err := r.List(10, time.Time{})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestReadRecordsErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := readRecords(filepath.Join(t.TempDir(), "missing.jsonl"))
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("skips blank lines", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.jsonl")
		content := `{"actor":"a","action":"one","success":true}

{"actor":"b","action":"two","success":false}
`
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		records, err := readRecords(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(records) != 2 {
			t.Fatalf("expected 2 records, got %d", len(records))
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.jsonl")
		if err := os.WriteFile(path, []byte("not json\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := readRecords(path)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), ":1:") {
			t.Fatalf("expected line number in error, got %v", err)
		}
	})
	t.Run("line too long", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.jsonl")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(make([]byte, 1024*1024+10))
		f.WriteString("\n")
		f.Close()
		_, err = readRecords(path)
		if err == nil {
			t.Fatal("expected scanner error")
		}
	})
}

func TestRecorderRotationsMore(t *testing.T) {
	t.Run("empty file skips rotation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.jsonl")
		if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
		recorder := NewRecorder(path, RecorderOptions{MaxBytes: 1, Backups: 1})
		if err := recorder.Append(Record{Actor: "a", Action: "empty", Success: true}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"action":"empty"`) {
			t.Fatalf("expected record, got %s", data)
		}
	})
	t.Run("backups zero removes file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.jsonl")
		// Backups must be negative because NewRecorder treats 0 as "use default".
		recorder := NewRecorder(path, RecorderOptions{MaxBytes: 1, Backups: -1})
		if err := recorder.Append(Record{Actor: "a", Action: "first", Target: "one", Success: true}); err != nil {
			t.Fatal(err)
		}
		if err := recorder.Append(Record{Actor: "a", Action: "second", Target: "two", Success: true}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != 1 {
			t.Fatalf("expected 1 line, got %d: %s", len(lines), data)
		}
		if !strings.Contains(lines[0], `"target":"two"`) {
			t.Fatalf("expected second record, got %s", data)
		}
	})
	t.Run("remove oldest error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.jsonl")
		recorder := NewRecorder(path, RecorderOptions{MaxBytes: 1, Backups: 2})
		if err := recorder.Append(Record{Actor: "a", Action: "first", Target: strings.Repeat("x", 80), Success: true}); err != nil {
			t.Fatal(err)
		}
		oldest := path + ".2"
		if err := os.MkdirAll(filepath.Join(oldest, "keep"), 0o700); err != nil {
			t.Fatal(err)
		}
		err := recorder.Append(Record{Actor: "a", Action: "second", Target: strings.Repeat("y", 80), Success: true})
		if err == nil {
			t.Fatal("expected error removing non-empty backup directory")
		}
	})
	t.Run("source stat error during rotation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "panel.jsonl")
		recorder := NewRecorder(path, RecorderOptions{MaxBytes: 1, Backups: 2})
		if err := recorder.Append(Record{Actor: "a", Action: "first", Target: strings.Repeat("x", 80), Success: true}); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path+".1", path+".1"); err != nil {
			t.Fatal(err)
		}
		err := recorder.Append(Record{Actor: "a", Action: "second", Target: strings.Repeat("y", 80), Success: true})
		if err == nil {
			t.Fatal("expected error from symlink loop during rotation")
		}
	})
}

func TestRecorderRedactsSlice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.jsonl")
	recorder := NewRecorder(path, RecorderOptions{})
	if err := recorder.Append(Record{
		Actor:   "alice",
		Action:  "user.update",
		Success: true,
		Details: map[string]any{
			"items": []any{
				map[string]any{"password": "secret"},
				"plain",
			},
			"safe": "visible",
		},
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, "secret") {
		t.Fatalf("secret leaked: %s", text)
	}
	if !strings.Contains(text, `"safe":"visible"`) {
		t.Fatalf("safe value missing: %s", text)
	}
	if !strings.Contains(text, `"items":[{"password":"[REDACTED]"},"plain"]`) {
		t.Fatalf("slice redaction wrong: %s", text)
	}
}

func TestRecorderAppendConcurrency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.jsonl")
	recorder := NewRecorder(path, RecorderOptions{})
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if err := recorder.Append(Record{
				Actor:   "alice",
				Action:  "concurrent",
				Target:  strconv.Itoa(i),
				Success: true,
			}); err != nil {
				t.Errorf("append %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	records, err := recorder.List(n, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != n {
		t.Fatalf("expected %d records, got %d", n, len(records))
	}
	seen := make(map[string]bool)
	for _, r := range records {
		seen[r.Target] = true
	}
	if len(seen) != n {
		t.Fatalf("duplicate or missing targets: %d unique", len(seen))
	}
}
