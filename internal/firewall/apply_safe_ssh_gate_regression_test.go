package firewall

import (
	"testing"
)

// Regression for #1007: install-time ApplySafely must not treat a
// deny/reject/limit (or outbound-only) rule on the SSH port as management
// access — such a rule does not pass SSH traffic once ufw is enabled.
func TestApplySafelyIgnoresNonAllowSSHRules(t *testing.T) {
	for _, action := range []string{"DENY", "REJECT", "LIMIT", "ALLOW OUT"} {
		t.Run(action, func(t *testing.T) {
			model := &safeUFWModel{
				rules:   map[string]string{"22/tcp": "OpenSSH"},
				actions: map[string]string{"22/tcp": action},
			}
			rules := []Rule{{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil panel"}}}
			if err := NewUFWApplierWithRunner(model).ApplySafely(rules); err == nil {
				t.Fatal("inactive ufw was enabled with only a non-allow ssh rule")
			}
			if model.enabled {
				t.Fatal("management lockout preflight failure still enabled ufw")
			}
			for _, mutation := range model.mutations {
				if mutation == "enable" {
					t.Fatalf("enable ran without SSH access: %v", model.mutations)
				}
			}
		})
	}
}

// #1007 companion: a real inbound ALLOW on the SSH port — or a comment that
// names ssh on any allowed port — still unblocks an otherwise-safe enable,
// while non-allow actions and unrelated ports do not.
func TestSnapshotHasSSHRequiresInboundAllow(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"22/tcp                     ALLOW       Anywhere                   # OpenSSH", true},
		{"22/tcp                     ALLOW IN    Anywhere                   # OpenSSH", true},
		{"22/tcp                     DENY        Anywhere                   # OpenSSH", false},
		{"22/tcp                     REJECT      Anywhere                   # OpenSSH", false},
		{"22/tcp                     LIMIT       Anywhere                   # OpenSSH", false},
		{"22/tcp                     ALLOW OUT   Anywhere                   # OpenSSH", false},
		{"443/tcp                    ALLOW       Anywhere                   # ssh tunnel", true},
		{"443/tcp                    DENY        Anywhere                   # ssh tunnel", false},
		{"2222/tcp                   ALLOW       Anywhere                   # other service", false},
	} {
		snap, err := parseUFWStatus("Status: inactive\n\n" + tc.line + "\n")
		if err != nil {
			t.Fatalf("parse %q: %v", tc.line, err)
		}
		if got := snapshotHasSSH(snap); got != tc.want {
			t.Fatalf("snapshotHasSSH(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}
