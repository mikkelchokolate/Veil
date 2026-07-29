package audit

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCriticalAuditSpoolsWithinBoundAndReplaysAfterSinkRecovery(t *testing.T) {
	root := t.TempDir()
	blockedParent := filepath.Join(root, "blocked")
	if err := os.WriteFile(blockedParent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	primary := filepath.Join(blockedParent, "panel.jsonl")
	spool := filepath.Join(root, "spool", "critical.jsonl")
	options := RecorderOptions{MaxBytes: 1 << 20, Backups: 1}
	value := reflect.ValueOf(&options).Elem()
	setAuditOptionField(t, value, "QueueCapacity", int64(8))
	setAuditOptionField(t, value, "SpoolPath", spool)
	setAuditOptionField(t, value, "MaxSpoolBytes", int64(1<<20))
	setAuditOptionField(t, value, "BackpressurePolicy", "spool_critical")

	recorder := NewRecorder(primary, options)
	critical := Record{Actor: "admin", Action: "backup.restore", Target: "restore-job", Success: true}
	if err := recorder.Append(critical); err != nil {
		t.Fatalf("critical event was dropped instead of durably spooled: %v", err)
	}
	info, err := os.Stat(spool)
	if err != nil {
		t.Fatalf("critical spool missing: %v", err)
	}
	if info.Size() <= 0 || info.Size() > 1<<20 {
		t.Fatalf("spool size=%d outside configured bound", info.Size())
	}

	if err := os.Remove(blockedParent); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(blockedParent, 0o700); err != nil {
		t.Fatal(err)
	}
	restarted := NewRecorder(primary, options)
	records, err := restarted.List(100, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range records {
		if record.Action == critical.Action && record.Target == critical.Target {
			found = true
		}
	}
	if !found {
		t.Fatalf("spooled critical event was not replayed after sink recovery: %+v", records)
	}
	if stat, err := os.Stat(spool); err == nil && stat.Size() != 0 {
		t.Fatalf("spool was not consumed after durable replay: %d bytes", stat.Size())
	}
}

func setAuditOptionField(t *testing.T, value reflect.Value, name string, fieldValue any) {
	t.Helper()
	field := value.FieldByName(name)
	if !field.IsValid() || !field.CanSet() {
		t.Errorf("RecorderOptions lacks required %s", name)
		return
	}
	switch field.Kind() {
	case reflect.String:
		field.SetString(fieldValue.(string))
	case reflect.Int, reflect.Int64:
		field.SetInt(fieldValue.(int64))
	default:
		t.Errorf("RecorderOptions.%s has unsupported kind %s", name, field.Kind())
	}
}
