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
	"backup.restore":      "backup.restore.start",
	"security.key.rotate": "security.key.rotate",
	"setup.complete":      "setup.complete",
	"user.update":         "user.update",
	"update.":             "update.apply",
	"auth.role":           "auth.role.change",
	"auth.setup":          "auth.setup.complete",
	"key.rotate":          "key.rotate",
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
	// The bare family names are also critical (exact-match arm).
	for _, action := range []string{"backup.restore", "security.key.rotate", "setup.complete", "user.update", "key.rotate"} {
		if !criticalAuditAction(action) {
			t.Errorf("criticalAuditAction(%q) = false", action)
		}
	}
	for _, action := range []string{"security.test", "auth.login", "backup.create", "client.create", "update_settings"} {
		if criticalAuditAction(action) {
			t.Errorf("non-critical %q must not spool", action)
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
	for _, action := range []string{"security.key.rotate", "backup.restore"} {
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
}
