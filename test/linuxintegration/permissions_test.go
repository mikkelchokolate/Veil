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

// TestIntegrationPanelPermissionMatrix covers the post-#601 ownership contract:
// runtime-shared trees are root:veil-proxy so the internet-facing units can
// read them, panel secrets are root:veil, panel state is veil:veil, and backup
// material stays root-only. The veil account additionally reads the shared
// trees through its supplementary veil-proxy membership (audit #466/#521).
func TestIntegrationPanelPermissionMatrix(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("permission matrix requires root")
	}
	panel := lookupIdentity(t, "veil")
	proxy := lookupIdentity(t, "veil-proxy")
	nobody := lookupIdentity(t, "nobody")

	root, err := os.MkdirTemp("", "veil-permission-matrix-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove permission fixture: %v", err)
		}
	})
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "var", "lib", "veil")
	writeFixture(t, filepath.Join(etcDir, "state.key"), "key")
	writeFixture(t, filepath.Join(etcDir, "veil.env"), "token")
	writeFixture(t, filepath.Join(etcDir, "backup.passphrase"), "passphrase")
	writeFixture(t, filepath.Join(etcDir, "generated", "caddy", "panel.Caddyfile"), "config")
	writeFixture(t, filepath.Join(etcDir, "panel", "tls.key"), "tlskey")
	writeFixture(t, filepath.Join(etcDir, "certs", "bundle.pem"), "bundle")
	writeFixture(t, filepath.Join(etcDir, "www", "index.html"), "index")
	writeFixture(t, filepath.Join(varDir, "state.json"), "state")
	writeFixture(t, filepath.Join(varDir, "sessions.json"), "sessions")
	writeFixture(t, filepath.Join(varDir, "backups", "daily.enc"), "backup")

	if err := hostaccess.Migrate(
		hostaccess.Paths{EtcDir: etcDir, VarDir: varDir},
		hostaccess.Identity{UID: panel.uid, GID: panel.gid, ProxyUID: proxy.uid, ProxyGID: proxy.gid},
		func() time.Time { return time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC) },
	); err != nil {
		t.Fatal(err)
	}

	// The panel account (veil, supplementary veil-proxy member): reads panel
	// secrets, owns panel state, reads the shared runtime trees; cannot read
	// root-only backup material and cannot write the shared trees.
	probeIdentity(t, "panel", panel, []int{proxy.gid}, etcDir, varDir, `
set -eu
test -r "$ETC/state.key"
test -r "$ETC/veil.env"
test -r "$ETC/generated/caddy/panel.Caddyfile"
test -r "$ETC/panel/tls.key"
test -r "$ETC/certs/bundle.pem"
test -r "$ETC/www/index.html"
test -r "$VAR/state.json"
printf '\nupdated' >> "$VAR/state.json"
touch "$VAR/staging/panel-write"
touch "$VAR/updates/panel-update"
! test -r "$ETC/backup.passphrase"
! test -r "$VAR/backups/daily.enc"
! touch "$ETC/generated/panel-write"
`)

	// The internet-facing runtime account (veil-proxy): reads the shared
	// trees only — no panel secrets, no panel state, no backup material
	// (audit #466).
	probeIdentity(t, "proxy", proxy, nil, etcDir, varDir, `
set -eu
test -r "$ETC/generated/caddy/panel.Caddyfile"
test -r "$ETC/panel/tls.key"
test -r "$ETC/certs/bundle.pem"
test -r "$ETC/www/index.html"
! test -r "$ETC/state.key"
! test -r "$ETC/veil.env"
! test -r "$ETC/backup.passphrase"
! test -r "$VAR/state.json"
! test -r "$VAR/sessions.json"
! test -r "$VAR/backups/daily.enc"
! touch "$ETC/generated/proxy-write"
! touch "$VAR/proxy-write"
`)

	// Unrelated users see nothing.
	probeIdentity(t, "nobody", nobody, nil, etcDir, varDir, `
set -eu
! test -r "$ETC/state.key"
! test -r "$ETC/veil.env"
! test -r "$ETC/generated/caddy/panel.Caddyfile"
! test -r "$ETC/panel/tls.key"
! test -r "$VAR/state.json"
! test -r "$VAR/backups/daily.enc"
`)

	// Directory modes and ownership land on the contract values.
	for _, dir := range []string{"generated", "tls", "certs", "www", "panel"} {
		assertOwnerMode(t, filepath.Join(etcDir, dir), 0, proxy.gid, 0o750)
	}
	for _, file := range []string{"state.key", "veil.env"} {
		assertOwnerMode(t, filepath.Join(etcDir, file), 0, panel.gid, 0o640)
	}
	assertOwnerMode(t, filepath.Join(etcDir, "backup.passphrase"), 0, 0, 0o600)
	for _, file := range []string{"state.json", "sessions.json"} {
		assertOwnerMode(t, filepath.Join(varDir, file), panel.uid, panel.gid, 0o600)
	}
	assertOwnerMode(t, filepath.Join(etcDir, "panel", "tls.key"), 0, proxy.gid, 0o640)
	assertOwnerMode(t, filepath.Join(etcDir, "generated", "caddy", "panel.Caddyfile"), 0, proxy.gid, 0o640)
}

type matrixIdentity struct {
	uid int
	gid int
}

func lookupIdentity(t *testing.T, name string) matrixIdentity {
	t.Helper()
	account, err := user.Lookup(name)
	if err != nil {
		t.Skipf("%s account unavailable: %v", name, err)
	}
	uid64, _ := strconv.ParseUint(account.Uid, 10, 32)
	gid64, _ := strconv.ParseUint(account.Gid, 10, 32)
	return matrixIdentity{uid: int(uid64), gid: int(gid64)}
}

func probeIdentity(t *testing.T, name string, ident matrixIdentity, supplementary []int, etcDir, varDir, script string) {
	t.Helper()
	groups := make([]uint32, 0, len(supplementary))
	for _, gid := range supplementary {
		groups = append(groups, uint32(gid))
	}
	command := exec.Command("/bin/sh", "-c", script)
	command.Env = append(os.Environ(), "ETC="+etcDir, "VAR="+varDir)
	command.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(ident.uid), Gid: uint32(ident.gid), Groups: groups},
	}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s permission probe: %v\n%s", name, err, output)
	}
}

func assertOwnerMode(t *testing.T, path string, wantUID, wantGID int, wantMode os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("stat %s: no unix stat", path)
	}
	if int(stat.Uid) != wantUID || int(stat.Gid) != wantGID || info.Mode().Perm() != wantMode {
		t.Fatalf("%s owner/mode = %d:%d %#o, want %d:%d %#o", path, stat.Uid, stat.Gid, info.Mode().Perm(), wantUID, wantGID, wantMode)
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
