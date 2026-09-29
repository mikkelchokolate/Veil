package hostenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadEnvFileParsesVeilEnvShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "veil.env")
	body := "# comment\n" +
		"VEIL_PANEL_PUBLIC_IP=203.0.113.9,2001:db8::7\n" +
		"VEIL_PANEL_LE_IP_CERT=0\n" +
		" VEIL_ACME_CA_URL = https://127.0.0.1:14000/dir \n" +
		"malformed line without equals\n" +
		"=empty-key\n" +
		"VEIL_ACME_INSECURE=1\n" +
		"VEIL_ACME_INSECURE=\n" // last occurrence wins
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ReadEnvFile(path)
	if got["VEIL_PANEL_PUBLIC_IP"] != "203.0.113.9,2001:db8::7" {
		t.Fatalf("public IP spec mangled: %q", got["VEIL_PANEL_PUBLIC_IP"])
	}
	if got["VEIL_PANEL_LE_IP_CERT"] != "0" {
		t.Fatalf("opt-out flag mangled: %q", got["VEIL_PANEL_LE_IP_CERT"])
	}
	if got["VEIL_ACME_CA_URL"] != "https://127.0.0.1:14000/dir" {
		t.Fatalf("CA URL not trimmed: %q", got["VEIL_ACME_CA_URL"])
	}
	if got["VEIL_ACME_INSECURE"] != "" {
		t.Fatalf("later empty assignment must win, got %q", got["VEIL_ACME_INSECURE"])
	}
	if _, ok := got[""]; ok {
		t.Fatal("empty key must be skipped")
	}
}

func TestReadEnvFileMissingYieldsEmptyMap(t *testing.T) {
	got := ReadEnvFile(filepath.Join(t.TempDir(), "absent.env"))
	if got == nil || len(got) != 0 {
		t.Fatalf("missing file must yield an empty map, got %v", got)
	}
}

func TestEnvOrFilePrefersProcessEnvironment(t *testing.T) {
	file := map[string]string{"VEIL_ACME_CA_URL": "https://persisted.example/dir"}
	t.Setenv("VEIL_ACME_CA_URL", "https://shell.example/dir")
	if got := EnvOrFile(file, "VEIL_ACME_CA_URL"); got != "https://shell.example/dir" {
		t.Fatalf("explicit env must win over the persisted file, got %q", got)
	}
	t.Setenv("VEIL_ACME_CA_URL", "   ")
	if got := EnvOrFile(file, "VEIL_ACME_CA_URL"); got != "https://persisted.example/dir" {
		t.Fatalf("blank env must fall back to the file, got %q", got)
	}
	os.Unsetenv("VEIL_ACME_CA_URL")
	if got := EnvOrFile(file, "VEIL_ACME_CA_URL"); got != "https://persisted.example/dir" {
		t.Fatalf("persisted value not used: %q", got)
	}
	if got := EnvOrFile(file, "VEIL_ACME_CA_ROOT"); got != "" {
		t.Fatalf("absent key must yield empty, got %q", got)
	}
}
