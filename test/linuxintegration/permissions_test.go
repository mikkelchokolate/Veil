//go:build linux && linuxintegration

package linuxintegration

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/hostaccess"
)

func TestIntegrationPanelPermissionMatrix(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("permission matrix requires root")
	}
	account, err := user.Lookup("nobody")
	if err != nil {
		t.Skipf("nobody account unavailable: %v", err)
	}
	uid64, _ := strconv.ParseUint(account.Uid, 10, 32)
	gid64, _ := strconv.ParseUint(account.Gid, 10, 32)
	panel := hostaccess.Identity{UID: int(uid64), GID: int(gid64)}

	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "var", "lib", "veil")
	writeFixture(t, filepath.Join(etcDir, "state.key"), "key")
	writeFixture(t, filepath.Join(etcDir, "veil.env"), "token")
	writeFixture(t, filepath.Join(etcDir, "backup.passphrase"), "passphrase")
	writeFixture(t, filepath.Join(etcDir, "generated", "caddy", "panel.Caddyfile"), "config")
	writeFixture(t, filepath.Join(varDir, "state.json"), "state")
	writeFixture(t, filepath.Join(varDir, "sessions.json"), "sessions")
	writeFixture(t, filepath.Join(varDir, "backups", "daily.enc"), "backup")

	if err := hostaccess.Migrate(
		hostaccess.Paths{EtcDir: etcDir, VarDir: varDir},
		panel,
		func() time.Time { return time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC) },
	); err != nil {
		t.Fatal(err)
	}

	script := `
set -eu
test -r "$ETC/state.key"
test -r "$ETC/veil.env"
test -r "$VAR/state.json"
printf '\nupdated' >> "$VAR/state.json"
touch "$VAR/staging/panel-write"
touch "$VAR/updates/panel-update"
! test -r "$ETC/backup.passphrase"
! test -r "$VAR/backups/daily.enc"
! touch "$ETC/generated/panel-write"
`
	command := exec.Command("/bin/sh", "-c", script)
	command.Env = append(os.Environ(), "ETC="+etcDir, "VAR="+varDir)
	command.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(panel.UID), Gid: uint32(panel.GID)},
	}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("panel permission probe: %v\n%s", err, output)
	}
}

func writeFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o666); err != nil {
		t.Fatal(err)
	}
}
