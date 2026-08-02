package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerBuildsNeverUseHostNetworking(t *testing.T) {
	root := filepath.Join("..", "..", "scripts", "ci")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".sh") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), "--network host") {
			t.Errorf("%s uses forbidden host networking", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDockerCIBackendDoesNotShareHostNetworkOrRuntimeNamespace(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "ci", "vm-run.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, forbidden := range []string{
		"--network host",
		"--cgroupns=host",
		"/sys/fs/cgroup:/sys/fs/cgroup",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("Docker CI backend must not use %q on a live host", forbidden)
		}
	}
	if strings.Count(script, "--network bridge") < 2 {
		t.Fatal("both simple and systemd Docker backends must use an isolated bridge network")
	}
	for _, required := range []string{"--cgroupns=private", "--tmpfs /run", "--tmpfs /run/lock"} {
		if !strings.Contains(script, required) {
			t.Fatalf("systemd Docker backend is missing isolation flag %q", required)
		}
	}
}

func TestHostedRuntimeProbeUsesPrivilegedBubblewrapSandbox(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "ci", "runtimes.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	for _, required := range []string{
		`command -v bwrap`,
		`[ ! -S /run/systemd/private ]`,
		`sudo -- "${veil_bin}" runtime install --only naiveproxy`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("hosted runtime installation is missing %q", required)
		}
	}
}
