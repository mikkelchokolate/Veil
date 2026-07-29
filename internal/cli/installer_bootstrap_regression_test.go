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

const exactInstallCommand = "curl -fsSL https://github.com/mikkelchokolate/Veil/releases/latest/download/install.sh | sh"

func TestDocumentationUsesExactOneCommandInstaller(t *testing.T) {
	for _, path := range []string{"../../README.md", "../../docs/install.md"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.Contains(text, exactInstallCommand) {
			t.Errorf("%s does not contain exact one-command install", path)
		}
		for _, forbidden := range []string{"| sudo sh", "| sudo bash", "bash ./bootstrap.sh", "cosign verify-blob --bundle bootstrap.sh.bundle"} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s contains forbidden/manual bootstrap fragment %q", path, forbidden)
			}
		}
	}
}

func TestPipedBootstrapVerifiesEveryPrivilegedPayloadBeforeSudo(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	if !strings.HasPrefix(script, "#!/bin/sh\n") {
		t.Fatalf("piped bootstrap must be POSIX sh")
	}
	sudoAt := strings.Index(script, "\nsudo env ")
	if sudoAt < 0 {
		t.Fatal("bootstrap has no final sudo handoff")
	}
	for _, marker := range []string{
		"COSIGN_AMD64_SHA256=", "sha256sum -c -", "install-privileged.sh.bundle",
		"checksums.txt.bundle", "veil.provenance.json.bundle",
		"cosign\" verify-blob", "provenance subject digest mismatch",
		"tar -xzf", "archive_digest=", "binary_digest=", "installer_digest=",
	} {
		at := strings.Index(script, marker)
		if at < 0 || at > sudoAt {
			t.Errorf("verification marker %q must occur before sudo", marker)
		}
	}
	if strings.Contains(script[:sudoAt], "sudo ") {
		t.Error("bootstrap invokes sudo before verification completes")
	}
	for _, handoff := range []string{"VEIL_INSTALLER_SHA256=", "VEIL_VERIFIED_ARCHIVE_SHA256=", "VEIL_VERIFIED_BINARY_SHA256=", "install-privileged.sh", "--local-bin"} {
		if !strings.Contains(script[sudoAt:], handoff) {
			t.Errorf("sudo handoff missing %q", handoff)
		}
	}
}

func TestPrivilegedInstallerVerifiesOwnBytesBeforeSideEffects(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	verifyAt := strings.Index(script, "VEIL_INSTALLER_SHA256")
	firstWrite := firstPositiveIndex(script, "mkdir -p", "install -m", "systemctl", "sudo")
	if verifyAt < 0 || firstWrite < 0 || verifyAt > firstWrite {
		t.Fatalf("privileged installer must verify exact bytes before side effects")
	}
	for _, marker := range []string{"sha256sum \"${BASH_SOURCE[0]}\"", "exact script digest mismatch", "VEIL_VERIFIED_BINARY_SHA256", "Verified binary handoff digest mismatch"} {
		if !strings.Contains(script, marker) {
			t.Errorf("privileged installer missing %q", marker)
		}
	}
}

func TestPrivilegedInstallerRejectsMissingOrMismatchedVerification(t *testing.T) {
	checkBash(t)
	root := t.TempDir()
	fakeBinary := filepath.Join(root, "veil")
	if err := os.WriteFile(fakeBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, digest string }{{"missing", ""}, {"mismatch", strings.Repeat("0", 64)}} {
		t.Run(test.name, func(t *testing.T) {
			destination := filepath.Join(root, test.name)
			command := exec.Command("bash", "../../scripts/install-privileged.sh", "--local-bin", fakeBinary, "--install-dir", destination, "--yes")
			command.Env = append(os.Environ(), "VEIL_INSTALLER_SHA256="+test.digest)
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("unverified privileged installer unexpectedly succeeded: %s", output)
			}
			if _, statErr := os.Stat(filepath.Join(destination, "veil")); !os.IsNotExist(statErr) {
				t.Fatalf("side effect occurred before verification: %v", statErr)
			}
		})
	}
}

func TestPrivilegedInstallerAllowsVerifiedDevelopmentPayloadOnlyExplicitly(t *testing.T) {
	checkBash(t)
	script, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(script))
	root := t.TempDir()
	fakeBinary := filepath.Join(root, "veil")
	if err := os.WriteFile(fakeBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", "../../scripts/install-privileged.sh", "--unsafe-development", "--dry-run", "--local-bin", fakeBinary, "--yes")
	command.Env = append(os.Environ(), "VEIL_INSTALLER_SHA256="+digest)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("exact verified development installer failed: %v\n%s", err, output)
	}
}

func firstPositiveIndex(text string, needles ...string) int {
	best := -1
	for _, needle := range needles {
		if at := strings.Index(text, needle); at >= 0 && (best < 0 || at < best) {
			best = at
		}
	}
	return best
}
