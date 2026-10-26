package repair

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
)

// collectNotices runs a plan build with the given options and returns the
// non-fatal findings it reported.
func collectNotices(t *testing.T, opts Options) ([]string, error) {
	t.Helper()
	var notices []string
	_, err := BuildPlanFromOptions(opts, PlanDependencies{
		Secret:     func(label string) string { return "repair-" + label },
		Executable: testExecutableFunc,
		LookPath:   testLookPath,
		Notify:     func(msg string) { notices = append(notices, msg) },
	})
	return notices, err
}

// stubFailingLEIssuance makes the Let's Encrypt IP-certificate step fail, the
// way a blocked :80, a failed acme.sh run or an unreachable public IP does.
func stubFailingLEIssuance(t *testing.T, cause error) {
	t.Helper()
	oldIssue := leIPCertIssueFunc
	leIPCertIssueFunc = func(context.Context, acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		return acmeip.IssuedCert{}, cause
	}
	oldResolver := repairLEPublicIPResolver
	repairLEPublicIPResolver = func(context.Context, string, *http.Client, []string) (net.IP, error) {
		return net.ParseIP("203.0.113.5"), nil
	}
	t.Cleanup(func() {
		leIPCertIssueFunc = oldIssue
		repairLEPublicIPResolver = oldResolver
	})
}

// directRepairOptions writes the veil.env of a direct-mode host whose
// certificate needs renewal and returns the options for a mutating repair.
func directRepairOptions(t *testing.T) Options {
	t.Helper()
	etcDir, varDir := t.TempDir(), t.TempDir()
	env := "VEIL_PANEL_ACCESS=direct\nVEIL_DOMAIN=panel.example.com\nVEIL_EMAIL=admin@example.com\n"
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte(env), 0o640); err != nil {
		t.Fatal(err)
	}
	return Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: t.TempDir(), Yes: true, LEIPCert: true}
}

// TestRepairNotifiesAFailedLECertificateRenewal locks #1500: the default path
// keeps the certificate in place and finishes, but it must say that the renewal
// failed — repair used to exit 0 with no warning at all.
func TestRepairNotifiesAFailedLECertificateRenewal(t *testing.T) {
	opts := directRepairOptions(t)
	stubFailingLEIssuance(t, os.ErrPermission)

	notices, err := collectNotices(t, opts)
	if err != nil {
		t.Fatalf("repair plan: %v", err)
	}
	certPath := filepath.Join(opts.EtcDir, "panel", "tls.crt")
	var found bool
	for _, notice := range notices {
		if strings.Contains(notice, "could not renew the Let's Encrypt IP certificate") &&
			strings.Contains(notice, certPath) && strings.Contains(notice, "permission denied") {
			found = true
		}
	}
	if !found {
		t.Fatalf("repair did not report the failed certificate renewal; notices = %v", notices)
	}
}

// TestRepairFailsWhenTheCertificateIsRequestedExplicitly keeps the other half of
// the contract: with --le-ip-cert or --le-ip-cert-port on this invocation the
// operator asked for the certificate, so a failure must fail the command.
func TestRepairFailsWhenTheCertificateIsRequestedExplicitly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*Options)
		flag  string
	}{
		{"le-ip-cert", func(o *Options) { o.LEIPCertSet = true }, "--le-ip-cert"},
		{"le-ip-cert-port", func(o *Options) { o.LEIPCertPortSet = true; o.LEIPCertPort = 8080 }, "--le-ip-cert-port"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := directRepairOptions(t)
			tc.apply(&opts)
			stubFailingLEIssuance(t, os.ErrPermission)

			_, err := collectNotices(t, opts)
			if err == nil {
				t.Fatal("repair reported success although the requested certificate could not be issued")
			}
			for _, want := range []string{"renew the Let's Encrypt IP certificate", "permission denied", tc.flag, "kept unchanged"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %v does not mention %q", err, want)
				}
			}
		})
	}
}

// TestRepairNotifiesAFailedCaddyJSONRender covers the second swallowed-error
// chain from #1500: the Caddy JSON re-render falls back to the profile config,
// which may differ from what Caddy is running, so the fallback is reported.
func TestRepairNotifiesAFailedCaddyJSONRender(t *testing.T) {
	etcDir, varDir := t.TempDir(), t.TempDir()
	env := "VEIL_PANEL_ACCESS=caddy\nVEIL_DOMAIN=panel.example.com\nVEIL_EMAIL=admin@example.com\n"
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte(env), 0o640); err != nil {
		t.Fatal(err)
	}
	oldProbe := repairCaddyCapabilitiesProbe
	repairCaddyCapabilitiesProbe = func(string) (caddycapabilities.CaddyCapabilities, error) {
		return caddycapabilities.CaddyCapabilities{}, os.ErrNotExist
	}
	t.Cleanup(func() { repairCaddyCapabilitiesProbe = oldProbe })

	notices, err := collectNotices(t, Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: t.TempDir(), Yes: true})
	if err != nil {
		t.Fatalf("repair plan: %v", err)
	}
	var found bool
	for _, notice := range notices {
		if strings.Contains(notice, "could not re-render the Caddy configuration") {
			found = true
		}
	}
	if !found {
		t.Fatalf("repair did not report the Caddy JSON fallback; notices = %v", notices)
	}
}
