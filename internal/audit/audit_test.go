package audit

import (
	"strings"
	"testing"
)

func TestLogAppendsJSONLine(t *testing.T) {
	path := t.TempDir() + "/audit/log.jsonl"
	if err := AppendEvent(path, Event{Action: "install.apply", BackupID: "b1", Success: true}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	body, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(string(body), `"action":"install.apply"`) || !strings.Contains(string(body), `"backupID":"b1"`) {
		t.Fatalf("body = %s", body)
	}
}
