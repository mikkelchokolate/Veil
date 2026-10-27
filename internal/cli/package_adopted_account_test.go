package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestPostinstallRejectsUnsafeAdoptedAccount runs the shipped helper: a
// pre-existing account is what a prior root compromise leaves behind, so
// postinstall must refuse a root uid, a login-range uid or a real login shell
// instead of installing a panel that runs as it (#1278).
func TestPostinstallRejectsUnsafeAdoptedAccount(t *testing.T) {
	body, err := os.ReadFile("../../packaging/scripts/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	code := strings.ReplaceAll(string(body), "\r\n", "\n")
	start := strings.Index(code, "validate_adopted_account() {")
	if start < 0 {
		t.Fatal("postinstall has no validate_adopted_account helper")
	}
	end := strings.Index(code[start:], "\n    # end: validate_adopted_account")
	if end < 0 {
		t.Fatal("could not find the end of validate_adopted_account")
	}
	helper := code[start : start+end]

	for _, tc := range []struct {
		name    string
		uid     string
		shell   string
		max     string
		wantErr string
	}{
		{"root uid", "0", "/usr/sbin/nologin", "999", "uid 0"},
		{"login-range uid", "4500", "/usr/sbin/nologin", "999", "outside the system account range"},
		{"login shell", "442", "/bin/bash", "999", "login shell"},
		{"clean account", "442", "/usr/sbin/nologin", "999", ""},
		{"false shell", "442", "/bin/false", "999", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := helper + "\nvalidate_adopted_account veil \"$1\" \"$2\" \"$3\"\n"
			command := exec.Command("sh", "-c", script, "sh", tc.uid, tc.shell, tc.max)
			output, err := command.CombinedOutput()
			text := string(output)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("clean account rejected: %v\n%s", err, text)
				}
				return
			}
			if err == nil || !strings.Contains(text, tc.wantErr) {
				t.Fatalf("uid %s shell %s: err=%v output=%s", tc.uid, tc.shell, err, text)
			}
		})
	}
}

// TestPostinstallRejectsUnsafeAdoptedGroup covers the group half of the same
// boundary: a pre-existing group with gid 0 makes the panel run and write state
// as root, and a gid above the system range is not what `groupadd --system`
// creates.
func TestPostinstallRejectsUnsafeAdoptedGroup(t *testing.T) {
	body, err := os.ReadFile("../../packaging/scripts/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	code := strings.ReplaceAll(string(body), "\r\n", "\n")
	start := strings.Index(code, "validate_adopted_group() {")
	if start < 0 {
		t.Fatal("postinstall has no validate_adopted_group helper")
	}
	end := strings.Index(code[start:], "\n    # end: validate_adopted_group")
	if end < 0 {
		t.Fatal("could not find the end of validate_adopted_group")
	}
	helper := code[start : start+end]

	for _, tc := range []struct {
		name    string
		gid     string
		wantErr string
	}{
		{"root gid", "0", "gid 0"},
		{"login-range gid", "4500", "outside the system group range"},
		{"clean group", "442", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := helper + "\nvalidate_adopted_group veil \"$1\" 999\n"
			command := exec.Command("sh", "-c", script, "sh", tc.gid)
			output, err := command.CombinedOutput()
			text := string(output)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("clean group rejected: %v\n%s", err, text)
				}
				return
			}
			if err == nil || !strings.Contains(text, tc.wantErr) {
				t.Fatalf("gid %s: err=%v output=%s", tc.gid, err, text)
			}
		})
	}
}
