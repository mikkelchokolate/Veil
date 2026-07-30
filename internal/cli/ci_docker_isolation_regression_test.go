package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
