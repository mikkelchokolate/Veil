package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPreremoveSkipsStopDisableOnUpgrade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX packaging maintainer script")
	}
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is required to execute preremove.sh")
	}

	root := t.TempDir()
	logPath := filepath.Join(root, "systemctl.log")
	stub := filepath.Join(root, "systemctl")
	stubBody := "#!/bin/sh\nprintf 'systemctl %s\\n' \"$*\" >> \"$SYSTEMCTL_LOG\"\nexit 0\n"
	if err := os.WriteFile(stub, []byte(stubBody), 0o755); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		t.Helper()
		if err := os.WriteFile(logPath, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(shell, append([]string{"../../packaging/scripts/preremove.sh"}, args...)...)
		cmd.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"), "SYSTEMCTL_LOG="+logPath)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("preremove.sh %v: %v\n%s", args, err, out)
		}
		body, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}

	for _, args := range [][]string{{"upgrade", "1.2.3"}, {"1"}, {"deconfigure"}} {
		log := run(args...)
		if strings.Contains(log, "disable") || strings.Contains(log, "stop") {
			t.Fatalf("preremove.sh %v must not stop/disable units on upgrade, log:\n%s", args, log)
		}
	}

	for _, args := range [][]string{{"remove"}, {"0"}} {
		log := run(args...)
		if !strings.Contains(log, "disable veil.service") || !strings.Contains(log, "stop veil.service") {
			t.Fatalf("preremove.sh %v must still stop/disable on remove, log:\n%s", args, log)
		}
	}
}
