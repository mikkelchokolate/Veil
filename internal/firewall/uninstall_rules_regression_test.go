package firewall

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

// uninstallUFWRunner is a scripted ufw: `status` returns the fixture and
// every other call is recorded. deleteFail names targets whose delete fails.
type uninstallUFWRunner struct {
	status     string
	notFound   bool
	statusErr  error
	deleteFail map[string]bool
	calls      [][]string
}

func (r *uninstallUFWRunner) Run(in veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	r.calls = append(r.calls, append([]string(nil), in.Command...))
	if r.notFound {
		return veilruntime.RuntimeCommandOutput{NotFound: true, Err: errors.New("ufw not found")}
	}
	if len(in.Command) >= 2 && in.Command[1] == "status" {
		return veilruntime.RuntimeCommandOutput{Output: r.status, Err: r.statusErr}
	}
	if len(in.Command) == 4 && in.Command[1] == "delete" && r.deleteFail[in.Command[3]] {
		return veilruntime.RuntimeCommandOutput{Output: "ERROR: boom", Err: errors.New("exit status 1")}
	}
	return veilruntime.RuntimeCommandOutput{}
}

func (r *uninstallUFWRunner) mutations() [][]string {
	var out [][]string
	for _, c := range r.calls {
		if len(c) >= 2 && c[1] != "status" {
			out = append(out, c)
		}
	}
	return out
}

const uninstallActiveStatus = `Status: active

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW       Anywhere                   # Veil management SSH
80/tcp                     ALLOW       Anywhere                   # Veil ACME challenge
2096/tcp                   ALLOW       Anywhere                   # Veil panel
34443/udp                  ALLOW       Anywhere                   # Veil Hysteria2
8080/tcp                   ALLOW       Anywhere                   # operator web app
5432/tcp                   ALLOW       Anywhere
22/tcp (v6)                ALLOW       Anywhere (v6)              # Veil management SSH
80/tcp (v6)                ALLOW       Anywhere (v6)              # Veil ACME challenge
2096/tcp (v6)              ALLOW       Anywhere (v6)              # Veil panel
34443/udp (v6)             ALLOW       Anywhere (v6)              # Veil Hysteria2
`

// TestRemoveManagedRulesDeletesEveryVeilAllowButSSH is the #1289 regression:
// uninstall must close every port Veil opened (panel, proxy ports and the
// ACME :80 allow that the stale pass treats as protected) while keeping the
// management SSH allow and never touching operator rules.
func TestRemoveManagedRulesDeletesEveryVeilAllowButSSH(t *testing.T) {
	runner := &uninstallUFWRunner{status: uninstallActiveStatus}
	removed, err := NewUFWApplierWithRunner(runner).RemoveManagedRules()
	if err != nil {
		t.Fatalf("RemoveManagedRules: %v", err)
	}
	want := []string{"2096/tcp", "34443/udp", "80/tcp"}
	if !reflect.DeepEqual(removed, want) {
		t.Fatalf("removed = %v, want %v", removed, want)
	}
	wantCalls := [][]string{
		{"ufw", "delete", "allow", "2096/tcp"},
		{"ufw", "delete", "allow", "34443/udp"},
		{"ufw", "delete", "allow", "80/tcp"},
		{"ufw", "reload"},
	}
	if got := runner.mutations(); !reflect.DeepEqual(got, wantCalls) {
		t.Fatalf("ufw mutations = %v, want %v", got, wantCalls)
	}
	for _, c := range runner.calls {
		joined := strings.Join(c, " ")
		for _, forbidden := range []string{"22/tcp", "8080/tcp", "5432/tcp", "enable", "disable"} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("uninstall cleanup must not run %q", joined)
			}
		}
	}
}

func TestRemoveManagedRulesInactiveUFWSkipsReload(t *testing.T) {
	runner := &uninstallUFWRunner{status: strings.Replace(uninstallActiveStatus, "Status: active", "Status: inactive", 1)}
	removed, err := NewUFWApplierWithRunner(runner).RemoveManagedRules()
	if err != nil || len(removed) != 3 {
		t.Fatalf("removed = %v, err = %v", removed, err)
	}
	for _, c := range runner.calls {
		if c[len(c)-1] == "reload" {
			t.Fatalf("inactive ufw must not be reloaded: %v", runner.calls)
		}
	}
}

func TestRemoveManagedRulesNoUFWIsNoop(t *testing.T) {
	runner := &uninstallUFWRunner{notFound: true}
	removed, err := NewUFWApplierWithRunner(runner).RemoveManagedRules()
	if err != nil || len(removed) != 0 {
		t.Fatalf("missing ufw must be a no-op, removed = %v err = %v", removed, err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("missing ufw must stop after the status probe: %v", runner.calls)
	}
}

// A ufw that is installed but cannot report status (e.g. not root) must fail
// closed: uninstall cannot prove the ports were closed.
func TestRemoveManagedRulesStatusFailureFailsClosed(t *testing.T) {
	runner := &uninstallUFWRunner{statusErr: errors.New("exit status 1"), status: "ERROR: You need to be root to run this script"}
	if _, err := NewUFWApplierWithRunner(runner).RemoveManagedRules(); err == nil || !strings.Contains(err.Error(), "read ufw status") {
		t.Fatalf("err = %v, want read ufw status failure", err)
	}
	if got := runner.mutations(); len(got) != 0 {
		t.Fatalf("no mutation may follow a failed status read: %v", got)
	}
}

// One failed delete must not strand the remaining rules, and the error must
// name the target that is still open.
func TestRemoveManagedRulesContinuesPastDeleteFailure(t *testing.T) {
	runner := &uninstallUFWRunner{status: uninstallActiveStatus, deleteFail: map[string]bool{"2096/tcp": true}}
	removed, err := NewUFWApplierWithRunner(runner).RemoveManagedRules()
	if err == nil || !strings.Contains(err.Error(), "delete 2096/tcp") {
		t.Fatalf("err = %v, want delete 2096/tcp failure", err)
	}
	if want := []string{"34443/udp", "80/tcp"}; !reflect.DeepEqual(removed, want) {
		t.Fatalf("removed = %v, want %v", removed, want)
	}
	last := runner.calls[len(runner.calls)-1]
	if strings.Join(last, " ") != "ufw reload" {
		t.Fatalf("successful deletes must still be reloaded, last call = %v", last)
	}
}
