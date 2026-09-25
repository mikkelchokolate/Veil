package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderBackupScheduleDropInQuotesPassphrasePath is the #1028
// regression: the operator-supplied passphrase path lands verbatim in
// ConditionPathExists/ReadOnlyPaths/ExecStart, so whitespace must be quoted
// and a literal % escaped to %% — an unescaped % specifier-expands at unit
// load and silently points the oneshot at a different file.
func TestRenderBackupScheduleDropInQuotesPassphrasePath(t *testing.T) {
	systemdDir := t.TempDir()
	passphrasePath := filepath.Join(t.TempDir(), "veil secrets", "backup%h.passphrase")
	slashed := filepath.ToSlash(passphrasePath)

	dropIn := renderBackupScheduleDropIn(systemdDir, passphrasePath)

	// The raw path must never appear unquoted/unescaped in a directive line.
	for _, line := range strings.Split(dropIn, "\n") {
		if strings.Contains(line, slashed) {
			t.Fatalf("drop-in line embeds the raw passphrase path: %q\ndrop-in:\n%s", line, dropIn)
		}
	}
	if !strings.Contains(dropIn, "%%h") {
		t.Fatalf("literal %% in passphrase path must render as %%%%:\n%s", dropIn)
	}
	// Whitespace forces quoting on the path-bearing directives.
	quoted := `"` + strings.ReplaceAll(slashed, "%", "%%") + `"`
	if !strings.Contains(dropIn, "ConditionPathExists="+quoted) {
		t.Fatalf("ConditionPathExists must carry the quoted+escaped path:\n%s", dropIn)
	}
	if !strings.Contains(dropIn, "ReadOnlyPaths="+quoted) {
		t.Fatalf("ReadOnlyPaths must carry the quoted+escaped path:\n%s", dropIn)
	}
	if !strings.Contains(dropIn, "--passphrase-file "+quoted) {
		t.Fatalf("ExecStart --passphrase-file must be quoted+escaped:\n%s", dropIn)
	}

	// Round-trip: the path read back from the rendered drop-in must resolve
	// to the real filesystem location (%% collapsed, quotes stripped).
	dropInDir := backupScheduleDropInDir(systemdDir)
	if err := os.MkdirAll(dropInDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupScheduleDropInPath(systemdDir), []byte(dropIn), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := scheduledPassphrasePathFromDropIn(systemdDir); got != filepath.Clean(passphrasePath) {
		t.Fatalf("scheduledPassphrasePathFromDropIn = %q, want %q", got, passphrasePath)
	}
}

// TestQuoteSystemdExecArgEscapesPercent pins the %% escape on the token
// renderer shared by every backup-schedule directive (issue #1028).
func TestQuoteSystemdExecArgEscapesPercent(t *testing.T) {
	if got := quoteSystemdExecArg("/etc/veil%h/backup.passphrase"); got != "/etc/veil%%h/backup.passphrase" {
		t.Fatalf("quoteSystemdExecArg = %q", got)
	}
	if got := quoteSystemdExecArg("/etc/veil dir/key"); got != `"/etc/veil dir/key"` {
		t.Fatalf("quoteSystemdExecArg space = %q", got)
	}
	if got := unquoteSystemdToken(`"/etc/veil%%h dir/key"`); got != "/etc/veil%h dir/key" {
		t.Fatalf("unquoteSystemdToken = %q", got)
	}
}
