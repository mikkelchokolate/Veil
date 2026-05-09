package audit

import "os"

type Event = AuditEvent

func AppendEvent(path string, event Event) error { return AppendAuditEvent(path, event) }
func Load(path string) ([]byte, error)           { return os.ReadFile(path) }
