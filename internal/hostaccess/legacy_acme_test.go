package hostaccess

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Package tests must never read or rewrite the crontab of the machine that
// runs them (CI runs some legs as root): Prepare goes through this fake
// unless a test installs its own.
func init() { testHooks.rootCrontab = &fakeRootCrontab{} }

type fakeRootCrontab struct {
	content  string
	exists   bool
	readErr  error
	writeErr error
	writes   []string
}

func (f *fakeRootCrontab) Read() (string, bool, error) { return f.content, f.exists, f.readErr }

func (f *fakeRootCrontab) Write(content string) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.writes = append(f.writes, content)
	f.content, f.exists = content, true
	return nil
}

func withRootCrontab(t *testing.T, cron RootCrontab) {
	t.Helper()
	original := testHooks.rootCrontab
	testHooks.rootCrontab = cron
	t.Cleanup(func() { testHooks.rootCrontab = original })
}

// legacyEntry is the exact line acme.sh 3.x `--install` registers for
// LE_WORKING_DIR=<home>/.acme.sh (the pre-#1240 Veil flow).
func legacyEntry(varDir string) string {
	wd := filepath.Join(varDir, "acme", ".acme.sh")
	return `41 0 * * * "` + wd + `"/acme.sh --cron --home "` + wd + `" > /dev/null`
}

func TestStripLegacyAcmeCronRemovesOnlyLegacyEntries(t *testing.T) {
	varDir := "/var/lib/veil"
	kept := strings.Join([]string{
		"# m h dom mon dow command",
		"",
		"# 41 0 * * * \"/var/lib/veil/acme/.acme.sh\"/acme.sh --cron (operator note)",
		`7 1 * * * "/root/.acme.sh"/acme.sh --cron --home "/root/.acme.sh" > /dev/null`,
		"0 4 * * * /usr/local/bin/backup.sh /var/lib/veil/acme-notes",
		"5 4 * * * /srv/var/lib/veil/acme/.acme.sh/acme.sh --cron",
		"6 4 * * * /var/lib/veil/acme/.acme.shx --cron",
	}, "\n")
	cron := &fakeRootCrontab{exists: true, content: strings.Join([]string{
		"# m h dom mon dow command",
		"",
		legacyEntry(varDir),
		"# 41 0 * * * \"/var/lib/veil/acme/.acme.sh\"/acme.sh --cron (operator note)",
		`7 1 * * * "/root/.acme.sh"/acme.sh --cron --home "/root/.acme.sh" > /dev/null`,
		"0 4 * * * /usr/local/bin/backup.sh /var/lib/veil/acme-notes",
		"13 2 * * * /var/lib/veil/acme/.acme.sh/acme.sh --cron --home /var/lib/veil/acme/.acme.sh >/dev/null\r",
		"5 4 * * * /srv/var/lib/veil/acme/.acme.sh/acme.sh --cron",
		"6 4 * * * /var/lib/veil/acme/.acme.shx --cron",
		"@daily HOME=/var/lib/veil/acme '/var/lib/veil/acme/.acme.sh/acme.sh' --cron",
	}, "\n") + "\n"}

	removed, err := StripLegacyAcmeCron(cron, varDir)
	if err != nil {
		t.Fatalf("strip: %v", err)
	}
	if removed != 3 {
		t.Fatalf("removed=%d, want 3", removed)
	}
	if len(cron.writes) != 1 {
		t.Fatalf("writes=%d, want 1", len(cron.writes))
	}
	if got, want := cron.writes[0], kept+"\n"; got != want {
		t.Fatalf("rewritten crontab:\n%q\nwant:\n%q", got, want)
	}

	// Second run: nothing left to remove, crontab not rewritten.
	removed, err = StripLegacyAcmeCron(cron, varDir)
	if err != nil || removed != 0 || len(cron.writes) != 1 {
		t.Fatalf("second run removed=%d err=%v writes=%d", removed, err, len(cron.writes))
	}
}

func TestStripLegacyAcmeCronFollowsCustomVarDir(t *testing.T) {
	cron := &fakeRootCrontab{exists: true, content: legacyEntry("/var/lib/veil") + "\n" + legacyEntry("/srv/veil-state/") + "\n"}
	removed, err := StripLegacyAcmeCron(cron, "/srv/veil-state/")
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if got := cron.writes[0]; got != legacyEntry("/var/lib/veil")+"\n" {
		t.Fatalf("rewritten crontab %q", got)
	}
}

func TestStripLegacyAcmeCronNoCrontabAndErrors(t *testing.T) {
	missing := &fakeRootCrontab{}
	if removed, err := StripLegacyAcmeCron(missing, "/var/lib/veil"); err != nil || removed != 0 || len(missing.writes) != 0 {
		t.Fatalf("no crontab: removed=%d err=%v writes=%d", removed, err, len(missing.writes))
	}
	clean := &fakeRootCrontab{exists: true, content: "0 1 * * * /bin/true\n"}
	if removed, err := StripLegacyAcmeCron(clean, "/var/lib/veil"); err != nil || removed != 0 || len(clean.writes) != 0 {
		t.Fatalf("clean crontab: removed=%d err=%v writes=%d", removed, err, len(clean.writes))
	}
	readFail := &fakeRootCrontab{readErr: errors.New("boom")}
	if _, err := StripLegacyAcmeCron(readFail, "/var/lib/veil"); err == nil {
		t.Fatal("a crontab read failure must fail closed")
	}
	writeFail := &fakeRootCrontab{exists: true, content: legacyEntry("/var/lib/veil") + "\n", writeErr: errors.New("boom")}
	if _, err := StripLegacyAcmeCron(writeFail, "/var/lib/veil"); err == nil {
		t.Fatal("a crontab write failure must fail closed")
	}
	if _, err := StripLegacyAcmeCron(clean, ""); err == nil {
		t.Fatal("empty var dir must be rejected")
	}
}

// Prepare is what `veil helper migrate` (package postinstall) and the
// installer run: it must drop the legacy cron job, and a failure to do so
// must abort instead of leaving the root exec path in place (#1245).
func TestPrepareStripsLegacyAcmeCronBeforeAccounts(t *testing.T) {
	varDir := t.TempDir()
	cron := &fakeRootCrontab{exists: true, content: "0 1 * * * /bin/true\n" + legacyEntry(varDir) + "\n"}
	withRootCrontab(t, cron)
	original := testHooks.prepareAccountDeps
	t.Cleanup(func() { testHooks.prepareAccountDeps = original })
	testHooks.prepareAccountDeps = func() AccountDependencies {
		return AccountDependencies{
			LookupUser:  func(string) (*user.User, error) { return nil, errors.New("no user") },
			LookupGroup: func(string) (*user.Group, error) { return nil, errors.New("no group") },
			Run:         func(string, ...string) error { return errors.New("no command") },
		}
	}
	if err := Prepare(Paths{EtcDir: t.TempDir(), VarDir: varDir}); err == nil {
		t.Fatal("expected the account step to fail")
	}
	if len(cron.writes) != 1 || cron.writes[0] != "0 1 * * * /bin/true\n" {
		t.Fatalf("legacy entry not removed: writes=%q", cron.writes)
	}

	withRootCrontab(t, &fakeRootCrontab{readErr: errors.New("crontab exploded")})
	err := Prepare(Paths{EtcDir: t.TempDir(), VarDir: varDir})
	if err == nil || !strings.Contains(err.Error(), "legacy acme.sh cron") {
		t.Fatalf("crontab failure must abort Prepare, got %v", err)
	}
}

func TestMigrateLocksDownLegacyAcmeHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership and mode test")
	}
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc")
	varDir := filepath.Join(root, "var")
	script := filepath.Join(varDir, "acme", ".acme.sh", "acme.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(script), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(script, 0o777); err != nil {
		t.Fatal(err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := strconv.Atoi(current.Uid)
	gid, _ := strconv.Atoi(current.Gid)
	paths := Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}
	if err := Migrate(paths, Identity{UID: uid, GID: gid}, time.Now); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertMode(t, filepath.Join(varDir, "acme"), 0o700)
	assertMode(t, filepath.Dir(script), 0o700)
	assertMode(t, script, 0o600)
}

func TestMigrateLeavesLegacyAcmeSymlinkAndMissingHomeAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink test")
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := strconv.Atoi(current.Uid)
	gid, _ := strconv.Atoi(current.Gid)

	// No legacy home: migration must not create one.
	root := t.TempDir()
	paths := Paths{EtcDir: filepath.Join(root, "etc"), VarDir: filepath.Join(root, "var"), RootUID: uid, RootGID: gid}
	if err := Migrate(paths, Identity{UID: uid, GID: gid}, time.Now); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(paths.VarDir, "acme")); !os.IsNotExist(err) {
		t.Fatalf("legacy acme home was created: %v", err)
	}

	// A planted symlink is neither followed nor fatal.
	target := t.TempDir()
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(paths.VarDir, "acme")); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(paths, Identity{UID: uid, GID: gid}, time.Now); err != nil {
		t.Fatalf("migrate with symlinked legacy home: %v", err)
	}
	assertMode(t, target, 0o755)
}

// The real crontab adapter: fixed binary path, root's table, "no crontab"
// treated as empty, other failures surfaced.
func TestSystemRootCrontabAdapter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script stand-in for crontab")
	}
	dir := t.TempDir()
	store := filepath.Join(dir, "table")
	argsLog := filepath.Join(dir, "args")
	bin := filepath.Join(dir, "crontab")
	scriptBody := `#!/bin/sh
echo "$@" >> "` + argsLog + `"
case "$3" in
-l)
	if [ -f "` + store + `" ]; then cat "` + store + `"; exit 0; fi
	echo "no crontab for root" >&2; exit 1 ;;
-)
	cat > "` + store + `"; exit 0 ;;
esac
echo "unexpected $*" >&2
exit 2
`
	if err := os.WriteFile(bin, []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	original := crontabCandidates
	crontabCandidates = []string{filepath.Join(dir, "missing"), bin}
	t.Cleanup(func() { crontabCandidates = original })

	cron := systemRootCrontab{}
	if _, exists, err := cron.Read(); err != nil || exists {
		t.Fatalf("no crontab: exists=%v err=%v", exists, err)
	}
	if err := os.WriteFile(store, []byte("0 1 * * * /bin/true\n"+legacyEntry("/var/lib/veil")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := StripLegacyAcmeCron(cron, "/var/lib/veil")
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	got, err := os.ReadFile(store)
	if err != nil || string(got) != "0 1 * * * /bin/true\n" {
		t.Fatalf("stored crontab %q err=%v", got, err)
	}
	logged, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	if want := "-u root -l\n-u root -l\n-u root -\n"; string(logged) != want {
		t.Fatalf("crontab invocations %q, want %q", logged, want)
	}

	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'permission denied' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cron.Read(); err == nil {
		t.Fatal("a crontab failure other than 'no crontab' must be an error")
	}

	crontabCandidates = []string{filepath.Join(dir, "missing")}
	if _, exists, err := cron.Read(); err != nil || exists {
		t.Fatalf("no crontab tool: exists=%v err=%v", exists, err)
	}
}
