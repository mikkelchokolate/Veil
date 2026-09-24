package audit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// criticalActionPrefixRE scrapes the prefix literals out of
// criticalAuditAction in recorder.go. The function is the authority for which
// action families spool; scraping keeps this test exhaustive — a new prefix
// added there must gain a representative probe here or the test fails.
var criticalActionPrefixRE = regexp.MustCompile(`"([^"]+)"`)

func criticalActionPrefixes(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile("recorder.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	start := strings.Index(src, "func criticalAuditAction")
	if start < 0 {
		t.Fatal("criticalAuditAction not found in recorder.go")
	}
	// "\n}" matches the closing brace under both LF and CRLF checkouts.
	end := strings.Index(src[start:], "\n}")
	if end < 0 {
		t.Fatal("criticalAuditAction body not terminated")
	}
	var prefixes []string
	for _, match := range criticalActionPrefixRE.FindAllStringSubmatch(src[start:start+end], -1) {
		prefixes = append(prefixes, match[1])
	}
	if len(prefixes) < 4 {
		t.Fatalf("scraped %d prefixes from criticalAuditAction — the scrape broke", len(prefixes))
	}
	return prefixes
}

// criticalActionProbes names one representative action per critical family.
// Live panel literals are used where the panel emits them today
// (security.key.rotate, setup.complete, user.update, backup.restore*); the
// update./auth.role/auth.setup/key.rotate families are spooled for the
// update/auth producers that emit dotted actions, so a representative member
// stands in for the family.
var criticalActionProbes = map[string]string{
	"auth.":                     "auth.login",
	"user.":                     "user.create",
	"setup.complete":            "setup.complete",
	"backup.":                   "backup.create",
	"update.":                   "update.apply",
	"security.key.":             "security.key.rotate",
	"key.rotate":                "key.rotate",
	"apply.rollback":            "apply.rollback",
	"install.apply":             "install.apply",
	"repair.apply":              "repair.apply",
	"rollback.":                 "rollback.runtime",
	"create_":                   "create_client",
	"update_":                   "update_settings",
	"delete_":                   "delete_inbound",
	"add_binding":               "add_binding",
	"remove_binding":            "remove_binding",
	"set_credential":            "set_credential",
	"rotate_credential":         "rotate_credential",
	"issue_subscription_token":  "issue_subscription_token",
	"revoke_subscription_token": "revoke_subscription_token",
	"rotate_subscription_token": "rotate_subscription_token",
	"migrate_legacy":            "migrate_legacy",
	"bulk_":                     "bulk_create",
	"service_":                  "service_restart",
}

func TestCriticalAuditActionMatchesLivePanelActions(t *testing.T) {
	// Every prefix family declared by criticalAuditAction must have an
	// explicit representative probe — a family left untested is a silent gap
	// in the durability guarantee (issue #924).
	for _, prefix := range criticalActionPrefixes(t) {
		probe, ok := criticalActionProbes[prefix]
		if !ok {
			t.Errorf("critical prefix %q has no representative probe in criticalActionProbes", prefix)
			continue
		}
		if !strings.HasPrefix(probe, prefix) && probe != prefix {
			t.Errorf("probe %q does not exercise prefix %q", probe, prefix)
			continue
		}
		if !criticalAuditAction(probe) {
			t.Errorf("criticalAuditAction(%q) = false", probe)
		}
	}
	// Representative live panel actions across every spooled family (#981):
	// bare family names exercise the exact-match arm, dotted members the
	// prefix arm.
	for _, action := range []string{
		"security.key.rotate",
		"setup.complete",
		"user.create",
		"user.update",
		"user.delete",
		"backup.create",
		"backup.prune",
		"backup.delete",
		"backup.verify",
		"backup.restore",
		"backup.restore.start",
		"auth.login",
		"auth.login.rate_limited",
		"auth.logout",
		"auth.session.revoke",
		"apply.rollback",
		"migrate_legacy",
		"set_credential",
		"rotate_credential",
		"issue_subscription_token",
		"revoke_subscription_token",
		"create_client",
		"update_settings",
		"delete_inbound",
		"service_restart",
		"key.rotate",
	} {
		if !criticalAuditAction(action) {
			t.Errorf("criticalAuditAction(%q) = false", action)
		}
	}
	for _, action := range []string{"security.test", "client.list", "request.test"} {
		if criticalAuditAction(action) {
			t.Errorf("non-critical %s must not spool", action)
		}
	}
}

func TestProductionAuditPathLayoutSpoolsCriticalEventsWithoutOptions(t *testing.T) {
	root := t.TempDir()
	auditDir := filepath.Join(root, "audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		t.Fatal(err)
	}
	primary := filepath.Join(auditDir, "panel.jsonl")
	if err := os.Mkdir(primary, 0o700); err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder(primary, RecorderOptions{})
	for _, action := range []string{"security.key.rotate", "backup.restore", "backup.create", "user.delete", "auth.login"} {
		if err := recorder.Append(Record{Actor: "admin", Action: action, Success: true}); err != nil {
			t.Fatalf("critical %s was dropped instead of spooled: %v", action, err)
		}
	}
	info, err := os.Stat(filepath.Join(auditDir, "critical.spool"))
	if err != nil {
		t.Fatalf("production spool missing: %v", err)
	}
	if info.Size() <= 0 {
		t.Fatal("production spool was not written")
	}
	if !recorder.SpoolDurable() {
		t.Fatal("recorder did not mark the accepted spool write durable")
	}
}

// TestSpoolDurableReflectsLastWrite (#981): a spool that has never accepted a
// write, or whose last write failed, must not report durable.
func TestSpoolDurableReflectsLastWrite(t *testing.T) {
	root := t.TempDir()
	// Primary is a directory so every primary append fails.
	primary := filepath.Join(root, "primary-dir")
	if err := os.Mkdir(primary, 0o700); err != nil {
		t.Fatal(err)
	}
	// Spool path is also a directory so spool writes fail too.
	spoolDir := filepath.Join(root, "spool-dir")
	if err := os.Mkdir(spoolDir, 0o700); err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder(primary, RecorderOptions{SpoolPath: spoolDir, BackpressurePolicy: "spool_critical"})
	if recorder.SpoolDurable() {
		t.Fatal("unwritten spool reported durable")
	}
	if err := recorder.Append(Record{Action: "backup.create", Success: true}); err == nil {
		t.Fatal("append with both sinks broken should fail")
	}
	if recorder.SpoolDurable() {
		t.Fatal("failed spool write reported durable")
	}
}
