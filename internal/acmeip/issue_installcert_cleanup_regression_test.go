package acmeip

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestInstallCertRejectionDropsOwnedACMEEnrollment pins #1246 item 3: when
// --installcert exits 0 but the installed material is rejected, the ACME
// enrollment this run created must be removed exactly as on an exec failure,
// so a later acme.sh renewal cannot run the reloadcmd against the rolled-back
// destination.
func TestInstallCertRejectionDropsOwnedACMEEnrollment(t *testing.T) {
	const ip = "192.0.2.30"
	certPath := "/etc/veil/panel/tls.crt"
	keyPath := "/etc/veil/panel/tls.key"

	cases := map[string]func(sys *fakeSystem){
		// acme.sh installs a cert for a different address.
		"wrong_ip": func(sys *fakeSystem) {
			sys.installIPs = []string{"198.51.100.9"}
		},
		// acme.sh installs an already-expired cert.
		"expired": func(sys *fakeSystem) {
			sys.issuedCertPEM, sys.issuedKeyPEM = generateIPCertPEM(time.Now().Add(-time.Hour), ip)
		},
		// The destination still holds the predecessor's bytes afterwards.
		// Synthetic: identical bytes normally mean the enrollment pre-existed
		// (and would be preserved); here no ACME dir exists so the test pins
		// the "previous material" rejection branch on its own.
		"unchanged": func(sys *fakeSystem) {
			cert, key := generateIPCertPEM(time.Now().Add(24*time.Hour), ip)
			sys.issuedCertPEM, sys.issuedKeyPEM = cert, key
			sys.fileData[certPath], sys.fileData[keyPath] = cert, key
			sys.files[certPath] = &fakeFileInfo{name: "tls.crt", mode: 0o600}
			sys.files[keyPath] = &fakeFileInfo{name: "tls.key", mode: 0o600}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			sys := newFakeSystem()
			sys.setAcmeInstalled()
			sys.lookPaths["socat"] = "/usr/bin/socat"
			setup(sys)

			acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
			sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
			sys.commands[sys.key(acmeSh, "--issue", "-d", ip, "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
			sys.commands[sys.key(acmeSh, "--installcert", "-d", ip, "--key-file", keyPath, "--fullchain-file", certPath, "--reloadcmd", renewReloadCmd(certPath, keyPath))] = commandResult{out: "Installed"}

			_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: ip, System: sys})
			requireInstallCertRejection(t, err)
			if !hasRunCall(sys, acmeSh, "--remove", "-d", ip) {
				t.Fatalf("rejected installcert left the ACME enrollment registered; run calls: %v", sys.runCalls)
			}
			if !hasRunCall(sys, "rm", "-rf", filepath.Join(sys.home, ".acme.sh", ip), filepath.Join(sys.home, ".acme.sh", ip+"_ecc")) {
				t.Fatalf("rejected installcert left the owned ACME directories; run calls: %v", sys.runCalls)
			}
		})
	}
}

// TestInstallCertRejectionPreservesExistingACMEEnrollment keeps the existing
// preserve rule on the newly covered path: an enrollment that predates this
// run (a renewal of an operator-managed cert) is never removed.
func TestInstallCertRejectionPreservesExistingACMEEnrollment(t *testing.T) {
	const ip = "192.0.2.31"
	certPath := "/etc/veil/panel/tls.crt"
	keyPath := "/etc/veil/panel/tls.key"

	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"
	sys.installIPs = []string{"198.51.100.9"}
	eccDir := filepath.Join(sys.home, ".acme.sh", ip+"_ecc")
	sys.files[eccDir] = &fakeFileInfo{name: ip + "_ecc", mode: os.ModeDir | 0o700}

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", ip, "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", ip, "--key-file", keyPath, "--fullchain-file", certPath, "--reloadcmd", renewReloadCmd(certPath, keyPath))] = commandResult{out: "Installed"}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: ip, System: sys})
	requireInstallCertRejection(t, err)
	for _, call := range sys.runCalls {
		if len(call) >= 2 && call[0] == acmeSh && call[1] == "--remove" {
			t.Fatalf("cleanup removed a pre-existing ACME enrollment: %v", call)
		}
		if len(call) >= 2 && call[0] == "rm" {
			for _, arg := range call[1:] {
				if arg == eccDir {
					t.Fatalf("cleanup removed the pre-existing ACME directory: %v", call)
				}
			}
		}
	}
}

// requireInstallCertRejection fails unless issuance failed in the
// post-installcert rejection branch (installcert exited 0), so a test cannot
// pass vacuously by failing at an earlier precondition or at the exec.
func requireInstallCertRejection(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected the rejected installcert to fail issuance")
	}
	if !strings.HasPrefix(err.Error(), "install certificate:") {
		t.Fatalf("issuance failed outside the installcert rejection branch: %v", err)
	}
	// The installcert exec-failure path shares the prefix but appends the
	// command output; requiring its absence pins "exit 0, material rejected"
	// so a drifted fake installcert key cannot pass via installErr.
	if strings.Contains(err.Error(), "(output:") {
		t.Fatalf("installcert exec failed instead of exiting 0 with rejected material: %v", err)
	}
}

func hasRunCall(sys *fakeSystem, want ...string) bool {
	for _, call := range sys.runCalls {
		if len(call) != len(want) {
			continue
		}
		match := true
		for i := range want {
			if call[i] != want[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
