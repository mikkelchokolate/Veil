package backupsftp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

func TestConfigValidate(t *testing.T) {
	base := Config{
		Enabled: true, Host: "backups.example.com", User: "veil",
		RemoteDir: "/srv/veil-backups", AuthType: AuthTypePassword, Password: "pw",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if base.Address() != "backups.example.com:22" {
		t.Fatalf("default port not applied: %s", base.Address())
	}

	cases := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"missing host", func(c *Config) { c.Host = "" }, "host is required"},
		{"host with slash", func(c *Config) { c.Host = "a/b" }, "hostname or IP"},
		{"host with whitespace", func(c *Config) { c.Host = "a b" }, "hostname or IP"},
		{"bad port", func(c *Config) { c.Port = 70000 }, "port"},
		{"missing user", func(c *Config) { c.User = "  " }, "user is required"},
		{"missing remote dir", func(c *Config) { c.RemoteDir = "" }, "remote directory"},
		{"bad auth type", func(c *Config) { c.AuthType = "token" }, "authType"},
		{"password missing", func(c *Config) { c.Password = "" }, "requires a password"},
		{
			"key missing path", func(c *Config) {
				c.AuthType = AuthTypeKey
				c.Password = ""
			}, "key path",
		},
		{
			"key relative path", func(c *Config) {
				c.AuthType = AuthTypeKey
				c.Password = ""
				c.KeyPath = "keys/id_ed25519"
			}, "absolute",
		},
		{
			"key under /root outside managed config dir", func(c *Config) {
				c.AuthType = AuthTypeKey
				c.Password = ""
				c.KeyPath = "/root/.ssh/id_ed25519"
			}, "must live under the managed config dir",
		},
		{
			"key under /home outside managed config dir", func(c *Config) {
				c.AuthType = AuthTypeKey
				c.Password = ""
				c.KeyPath = "/home/veil/.ssh/id_ed25519"
			}, "must live under the managed config dir",
		},
		{
			"key under state dir rejected", func(c *Config) {
				c.AuthType = AuthTypeKey
				c.Password = ""
				c.KeyPath = "/var/lib/veil/id_ed25519"
			}, "must live under the managed config dir",
		},
		{
			"key path escaping the managed dir rejected", func(c *Config) {
				c.AuthType = AuthTypeKey
				c.Password = ""
				c.KeyPath = "/etc/veil/../shadow"
			}, "must live under the managed config dir",
		},
		{
			"key under a sibling etc prefix rejected", func(c *Config) {
				c.AuthType = AuthTypeKey
				c.Password = ""
				c.KeyPath = "/etc/veil2/id_ed25519"
			}, "must live under the managed config dir",
		},
	}
	for _, tc := range cases {
		config := base
		tc.mutate(&config)
		err := config.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err=%v, want substring %q", tc.name, err, tc.want)
		}
	}
}

// #1229-F7: a key path is only valid inside the managed config dir, so the
// privileged helper never reads or signs with an operator-unmanaged file.
// The managed dir resolves through hostenv so custom installs keep working.
func TestConfigValidateKeyPathConfinedToEtcDir(t *testing.T) {
	etcDir := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etcDir)
	keyPath := filepath.Join(etcDir, "backup-sftp.key")
	config := Config{
		Enabled: true, Host: "backups.example.com", User: "veil",
		RemoteDir: "/srv/veil-backups", AuthType: AuthTypeKey,
		KeyPath: keyPath,
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("managed-dir key path rejected: %v", err)
	}
	config.KeyPath = filepath.Join(etcDir, "keys", "backup-sftp.key")
	if err := config.Validate(); err != nil {
		t.Fatalf("nested managed-dir key path rejected: %v", err)
	}
	config.KeyPath = filepath.Join(t.TempDir(), "id_ed25519")
	if err := config.Validate(); err == nil {
		t.Fatal("key path outside the managed config dir accepted")
	}
}

func TestConfigSaveLoadRoundtripKeepsSecretsLocal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFileName)
	// Pin the managed config root so hostenv env leakage cannot move the
	// key-path confinement target (#1229).
	t.Setenv("VEIL_ETC_DIR", hostenv.DefaultEtcDir)
	config := Config{
		Enabled: true, Host: "backups.example.com", Port: 2222, User: "veil",
		RemoteDir: "/srv/veil-backups", AuthType: AuthTypeKey,
		KeyPath: "/etc/veil/backup-sftp.key", KeyPassphrase: "key-secret",
		Password: "never-here", HostKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDb8mQJYHLqAoBzBfOkJM0iOOcXLbbQ1sDHs/RTmkrqX",
	}
	if err := SaveConfig(path, config); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 0600", info.Mode().Perm())
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || *loaded != config {
		t.Fatalf("roundtrip mismatch: %+v", loaded)
	}

	// The public view must carry flags only — never the secret values.
	view := loaded.PublicView()
	body, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"key-secret", "never-here", "AAAAC3NzaC1lZDI1NTE5"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("public view leaked a secret: %s", body)
		}
	}
	if !view.PasswordSet || !view.KeyPassphraseSet || !view.HostKeySet || !view.Configured {
		t.Fatalf("public view flags wrong: %+v", view)
	}
}

func TestConfigSaveNormalizesPortAndRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFileName)
	config := Config{
		Enabled: true, Host: "h", User: "u", RemoteDir: "/r",
		AuthType: AuthTypePassword, Password: "pw",
	}
	if err := SaveConfig(path, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Port != DefaultPort {
		t.Fatalf("port = %d, want %d", loaded.Port, DefaultPort)
	}
	config.Host = ""
	if err := SaveConfig(path, config); err == nil {
		t.Fatal("invalid config was persisted")
	}
}

func TestConfigLoadMissingAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	missing, err := LoadConfig(filepath.Join(dir, "absent.json"))
	if err != nil || missing != nil {
		t.Fatalf("missing config = %v, %v", missing, err)
	}
	corrupt := filepath.Join(dir, ConfigFileName)
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(corrupt); err == nil {
		t.Fatal("corrupt config loaded without error")
	}
}

func TestDeleteConfigMissingIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), ConfigFileName)
	if err := DeleteConfig(path); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(path, Config{
		Enabled: true, Host: "h", User: "u", RemoteDir: "/r",
		AuthType: AuthTypePassword, Password: "pw",
	}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteConfig(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("config still present: %v", err)
	}
}

func TestStatusRoundtripAndMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), StatusFileName)
	if status := LoadStatus(path); status != (Status{}) {
		t.Fatalf("missing status = %+v", status)
	}
	want := Status{LastUploadAt: "2026-06-05T02:00:11Z", LastUploadArchive: "a.tar.gz.enc", LastError: "boom", LastErrorAt: "2026-06-05T02:01:00Z"}
	if err := SaveStatus(path, want); err != nil {
		t.Fatal(err)
	}
	if got := LoadStatus(path); got != want {
		t.Fatalf("status roundtrip = %+v, want %+v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("status mode = %o, want 0600", info.Mode().Perm())
	}
}
