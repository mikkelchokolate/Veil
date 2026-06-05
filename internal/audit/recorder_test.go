package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecorderRedactsNestedSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.jsonl")
	recorder := NewRecorder(path, RecorderOptions{})
	event := Record{
		Actor:   "alice",
		Role:    "admin",
		Action:  "user.update",
		Target:  "bob",
		Success: true,
		Details: map[string]any{
			"password": "do-not-write",
			"nested": map[string]any{
				"apiToken":      "also-secret",
				"authorization": "Bearer hidden",
				"safe":          "visible",
			},
		},
	}
	if err := recorder.Append(event); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, secret := range []string{"do-not-write", "also-secret", "Bearer hidden"} {
		if strings.Contains(text, secret) {
			t.Fatalf("audit log contains secret %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, `"safe":"visible"`) || !strings.Contains(text, `"password":"[REDACTED]"`) {
		t.Fatalf("redacted audit record = %s", text)
	}
}

func TestRecorderRotatesAndKeepsBoundedGenerations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.jsonl")
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	recorder := NewRecorder(path, RecorderOptions{
		MaxBytes: 180,
		Backups:  2,
		Now: func() time.Time {
			now = now.Add(time.Second)
			return now
		},
	})
	for i := 0; i < 8; i++ {
		if err := recorder.Append(Record{
			Actor:   "alice",
			Action:  "rotation.test",
			Target:  strings.Repeat("x", 80),
			Success: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, candidate := range []string{path, path + ".1", path + ".2"} {
		if _, err := os.Stat(candidate); err != nil {
			t.Fatalf("expected rotated file %s: %v", candidate, err)
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("rotation exceeded configured generations: %v", err)
	}
}

func TestRecorderListReturnsNewestRecordsAcrossRotations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.jsonl")
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	recorder := NewRecorder(path, RecorderOptions{
		MaxBytes: 220,
		Backups:  3,
		Now: func() time.Time {
			now = now.Add(time.Minute)
			return now
		},
	})
	for _, target := range []string{"one", "two", "three", "four", "five"} {
		if err := recorder.Append(Record{Actor: "alice", Action: "list.test", Target: target, Success: true}); err != nil {
			t.Fatal(err)
		}
	}
	records, err := recorder.List(3, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 || records[0].Target != "five" || records[1].Target != "four" || records[2].Target != "three" {
		t.Fatalf("newest records = %+v", records)
	}

	before := records[1].Timestamp
	older, err := recorder.List(10, before)
	if err != nil {
		t.Fatal(err)
	}
	if len(older) != 3 || older[0].Target != "three" {
		t.Fatalf("records before %s = %+v", before, older)
	}
}
