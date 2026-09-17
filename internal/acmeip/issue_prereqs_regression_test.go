package acmeip

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// Audit #298: acme.sh's installer refuses without a cron scheduler and its key
// generation fails without OpenSSL, so both must be provisioned before the
// upstream install runs — not discovered as a post-failure self-signed
// fallback.

func TestIssueIPCertProvisionsCronAndOpenSSLBeforeAcmeInstall(t *testing.T) {
	sys := newFakeSystem()
	delete(sys.lookPaths, "openssl")
	delete(sys.lookPaths, "crontab")
	delete(sys.lookPaths, "socat")
	sys.lookPaths["apt-get"] = "/usr/bin/apt-get"

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key("sh", "-c", "apt-get update >/dev/null 2>&1 && apt-get install -y openssl")] = commandResult{out: "done"}
	sys.commands[sys.key("sh", "-c", "apt-get update >/dev/null 2>&1 && apt-get install -y cron")] = commandResult{out: "done"}
	sys.commands[sys.key("sh", "-c", "apt-get update >/dev/null 2>&1 && apt-get install -y socat")] = commandResult{out: "done"}
	sys.commands[sys.key("sh", "-c", "curl -fsSL https://get.acme.sh | sh")] = commandResult{out: "installed"}
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{out: "Installed"}

	if _, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	indexOf := func(substr string) int {
		for i, call := range sys.execCalls {
			if strings.Contains(call, substr) {
				return i
			}
		}
		return -1
	}
	installAcme := indexOf("get.acme.sh")
	if installAcme < 0 {
		t.Fatalf("acme.sh install never ran: %v", sys.execCalls)
	}
	for _, prereq := range []string{"install -y openssl", "install -y cron", "install -y socat"} {
		idx := indexOf(prereq)
		if idx < 0 {
			t.Fatalf("prerequisite install %q never ran: %v", prereq, sys.execCalls)
		}
		if idx > installAcme {
			t.Fatalf("prerequisite %q ran after acme.sh install: %v", prereq, sys.execCalls)
		}
	}
}

func TestIssueIPCertFailsWhenCrontabCannotBeProvisioned(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["socat"] = "/usr/bin/socat"
	delete(sys.lookPaths, "crontab")
	// No package manager available at all.

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err == nil {
		t.Fatal("expected prerequisite failure without crontab")
	}
	if !strings.Contains(err.Error(), "acme prerequisites") || !strings.Contains(err.Error(), "crontab") {
		t.Fatalf("error must name the missing scheduler prerequisite, got: %v", err)
	}
	for _, call := range sys.execCalls {
		if strings.Contains(call, "get.acme.sh") {
			t.Fatalf("acme.sh install must not run when prerequisites fail: %v", sys.execCalls)
		}
	}
}

func TestIssueIPCertFailsWhenOpenSSLCannotBeProvisioned(t *testing.T) {
	sys := newFakeSystem()
	delete(sys.lookPaths, "openssl")

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err == nil {
		t.Fatal("expected prerequisite failure without openssl")
	}
	if !strings.Contains(err.Error(), "openssl") {
		t.Fatalf("error must name the missing crypto tool, got: %v", err)
	}
}

func TestEnsureAcmePrereqsNoopWhenToolsExist(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["socat"] = "/usr/bin/socat"
	if err := ensureAcmePrereqs(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sys.execCalls) != 0 {
		t.Fatalf("existing tools must not trigger installs: %v", sys.execCalls)
	}
}

func TestEnsureAcmePrereqsReportsCronInstallFailure(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["socat"] = "/usr/bin/socat"
	delete(sys.lookPaths, "crontab")
	sys.lookPaths["apt-get"] = "/usr/bin/apt-get"
	sys.commands[sys.key("sh", "-c", "apt-get update >/dev/null 2>&1 && apt-get install -y cron")] = commandResult{err: errors.New("package not found")}

	err := ensureAcmePrereqs(context.Background(), sys)
	if err == nil || !strings.Contains(err.Error(), "cron") {
		t.Fatalf("expected cron install failure, got: %v", err)
	}
}
