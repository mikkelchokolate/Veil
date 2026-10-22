package cli

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCIAptMirrorRoutingPreservesSecuritySources(t *testing.T) {
	checkBash(t)
	body, err := os.ReadFile("../../scripts/ci/apt-bootstrap.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	start := strings.Index(script, "case \"$(dpkg")
	if start < 0 {
		t.Fatal("apt architecture routing missing")
	}
	end := strings.Index(script[start:], "esac") + start + len("esac")
	helperStart := strings.Index(script, "rewrite_ubuntu_uri() {")
	helperEnd := strings.Index(script, "\nrewrite_ubuntu_uri /etc/apt/sources.list")
	if start < 0 || end <= start || helperStart < 0 || helperEnd <= helperStart {
		t.Fatal("apt mirror routing or source rewrite helper missing")
	}
	for arch, mirror := range map[string]string{
		"amd64": "http://archive.ubuntu.com/ubuntu",
		"arm64": "http://ports.ubuntu.com/ubuntu-ports",
	} {
		t.Run(arch, func(t *testing.T) {
			dir := t.TempDir()
			security := "URIs: http://security.ubuntu.com/ubuntu\nSuites: noble-security\nComponents: main restricted universe multiverse\n"
			ordinary := "URIs: http://azure.archive.ubuntu.com/ubuntu\nSuites: noble noble-updates noble-backports\n"
			for name, contents := range map[string]string{"security.sources": security, "ubuntu.sources": ordinary} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command("bash", "-c", "set -euo pipefail\ndpkg() { printf '%s\\n' \"$CI_TEST_ARCH\"; }\n"+
				script[start:end]+"\nprintf '%s\\n' \"$APT_MIRROR_URIS\"\nSUDO=\n"+script[helperStart:helperEnd]+
				"\nrewrite_ubuntu_uri \"$CI_APT_FIXTURE/security.sources\"\nrewrite_ubuntu_uri \"$CI_APT_FIXTURE/ubuntu.sources\"\n")
			command.Env = append(os.Environ(), "CI_TEST_ARCH="+arch, "CI_APT_FIXTURE="+filepath.ToSlash(dir))
			out, err := command.CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != mirror+"/" {
				t.Fatalf("mirror routing: %v: %s", err, out)
			}
			for name, want := range map[string]string{
				"security.sources": security,
				"ubuntu.sources":   strings.ReplaceAll(ordinary, "http://azure.archive.ubuntu.com/ubuntu", mirror),
			} {
				got, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || string(got) != want {
					t.Fatalf("source %s changed pockets or security route: %v: %s", name, err, got)
				}
			}
		})
	}
}

func TestCIBrowserPreparedDependenciesRequireMatchingLockAndPin(t *testing.T) {
	checkBash(t)
	body, err := os.ReadFile("../../scripts/ci/browser-e2e.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	start := strings.Index(script, "ci_step \"pinned Playwright")
	end := strings.Index(script, "# Browsers are baked")
	if start < 0 || end <= start {
		t.Fatal("browser dependency preparation block missing")
	}
	lock := []byte(`{"lockfileVersion":3}`)
	lockSHA := fmt.Sprintf("%x", sha256.Sum256(lock))
	for _, tc := range []struct {
		name, prepared, installed string
		wantInstall, wantError    bool
	}{
		{"workflow-prepared", lockSHA, "1.64.1", false, false},
		{"local-vm-install", "", "1.64.1", true, false},
		{"changed-lock", "stale", "1.64.1", true, false},
		{"wrong-installed-pin", lockSHA, "1.0.0", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			browserDir := filepath.Join(dir, "test", "browser")
			if err := os.MkdirAll(browserDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(browserDir, "package-lock.json"), lock, 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("bash", "-c", "set -euo pipefail\nci_step() { :; }\nci_die() { echo \"$*\" >&2; exit 1; }\n"+
				"npm() { printf '%s\\n' \"$*\" > npm-ci-called; }\nnode() { printf '%s\\n' \"$CI_TEST_INSTALLED_PIN\"; }\n"+script[start:end])
			command.Dir = dir
			command.Env = append(os.Environ(), "CI_PLAYWRIGHT_VERSION=1.64.1", "CI_TEST_INSTALLED_PIN="+tc.installed,
				"CI_BROWSER_DEPS_LOCK_SHA256="+tc.prepared)
			out, err := command.CombinedOutput()
			if (err != nil) != tc.wantError {
				t.Fatalf("prepared dependency check: %v: %s", err, out)
			}
			install, readErr := os.ReadFile(filepath.Join(browserDir, "npm-ci-called"))
			if (readErr == nil) != tc.wantInstall || (readErr == nil && strings.TrimSpace(string(install)) != "ci --ignore-scripts") {
				t.Fatalf("npm install=%q err=%v, want install=%v", install, readErr, tc.wantInstall)
			}
		})
	}
}
