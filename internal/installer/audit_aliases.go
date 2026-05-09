package installer

import "github.com/veil-panel/veil/internal/audit"

type AuditEvent = audit.AuditEvent

func AppendAuditEvent(path string, event AuditEvent) error {
	return audit.AppendAuditEvent(path, event)
}
