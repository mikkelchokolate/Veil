package audit

import (
	"encoding/json"
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
	var decoded Event
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("audit line is not valid JSON: %v\nbody: %s", err, body)
	}
	if decoded.Action != "install.apply" || decoded.BackupID != "b1" || !decoded.Success {
		t.Fatalf("audit record = %+v, want action=install.apply backupID=b1 success=true", decoded)
	}
}
