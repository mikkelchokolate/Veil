package audit

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLogUserAction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user.jsonl")

	event := UserAuditEvent{
		Username:  "alice",
		Role:      "admin",
		Action:    "login",
		Target:    "panel",
		IPAddress: "127.0.0.1",
		Success:   true,
		Details:   "otp verified",
	}
	if err := LogUserAction(path, event); err != nil {
		t.Fatalf("LogUserAction: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(data)
	for _, want := range []string{`"username":"alice"`, `"action":"login"`, `"ip":"127.0.0.1"`, `"details":"otp verified"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %s in %s", want, text)
		}
	}
}

func TestLogUserActionEmptyPath(t *testing.T) {
	if err := LogUserAction("", UserAuditEvent{Action: "x"}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestLogUserActionErrors(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*testing.T) string
		wantErr bool
	}{
		{
			name: "mkdir fails when parent is a file",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				block := filepath.Join(dir, "block")
				if err := os.WriteFile(block, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(block, "user.jsonl")
			},
			wantErr: true,
		},
		{
			name: "open fails when path is a directory",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				block := filepath.Join(dir, "blockdir")
				if err := os.Mkdir(block, 0o700); err != nil {
					t.Fatal(err)
				}
				return block
			},
			wantErr: true,
		},
		{
			name: "write fails on /dev/full",
			setup: func(t *testing.T) string {
				if runtime.GOOS != "linux" {
					t.Skip("/dev/full is Linux-specific")
				}
				return "/dev/full"
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.setup(t)
			err := LogUserAction(path, UserAuditEvent{Username: "u", Action: "a"})
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}

	t.Run("marshal error", func(t *testing.T) {
		orig := jsonMarshal
		jsonMarshal = func(any) ([]byte, error) { return nil, errors.New("marshal fail") }
		defer func() { jsonMarshal = orig }()

		err := LogUserAction(filepath.Join(t.TempDir(), "user.jsonl"), UserAuditEvent{Action: "a"})
		if err == nil {
			t.Fatalf("expected error")
		}
	})
}

func TestAppendAuditEventMoreErrors(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*testing.T) string
		wantErr bool
	}{
		{
			name: "mkdir fails when parent is a file",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				block := filepath.Join(dir, "block")
				if err := os.WriteFile(block, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(block, "audit.jsonl")
			},
			wantErr: true,
		},
		{
			name: "open fails when path is a directory",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				block := filepath.Join(dir, "blockdir")
				if err := os.Mkdir(block, 0o700); err != nil {
					t.Fatal(err)
				}
				return block
			},
			wantErr: true,
		},
		{
			name: "write fails on /dev/full",
			setup: func(t *testing.T) string {
				if runtime.GOOS != "linux" {
					t.Skip("/dev/full is Linux-specific")
				}
				return "/dev/full"
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.setup(t)
			err := AppendAuditEvent(path, AuditEvent{Action: "a", BackupID: "b1", Success: true})
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}

	t.Run("marshal error", func(t *testing.T) {
		orig := jsonMarshal
		jsonMarshal = func(any) ([]byte, error) { return nil, errors.New("marshal fail") }
		defer func() { jsonMarshal = orig }()

		err := AppendAuditEvent(filepath.Join(t.TempDir(), "audit.jsonl"), AuditEvent{Action: "a"})
		if err == nil {
			t.Fatalf("expected error")
		}
	})
}
