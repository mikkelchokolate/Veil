package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fakeSmolvmHarness struct {
	t        *testing.T
	repoRoot string
	fakeBin  string
	stateDir string
}

func newFakeSmolvmHarness(t *testing.T) *fakeSmolvmHarness {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	h := &fakeSmolvmHarness{t: t, repoRoot: repoRoot, fakeBin: t.TempDir(), stateDir: t.TempDir()}

	writeExecutable := func(name, body string) {
		t.Helper()
		path := filepath.Join(h.fakeBin, name)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	writeExecutable("uname", `#!/usr/bin/env bash
case "${1:-}" in
  -m) echo x86_64 ;;
  -s) echo Darwin ;;
  *) /usr/bin/uname "$@" ;;
esac
`)
	writeExecutable("docker", `#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  info) exit 0 ;;
  image) [ "${2:-}" = inspect ] && exit 0 ;;
  create) echo fake-export-container ;;
  export) /usr/bin/tar -cf - --files-from /dev/null ;;
  rm) exit 0 ;;
  *) echo "unexpected fake docker command: $*" >&2; exit 90 ;;
esac
`)
	writeExecutable("smolvm", `#!/usr/bin/env bash
set -euo pipefail
printf '%q ' "$@" >> "${FAKE_SMOLVM_LOG}"
printf '\n' >> "${FAKE_SMOLVM_LOG}"
if [ "${1:-}" = "--version" ]; then
  echo 'smolvm 1.6.13'
  exit 0
fi
if [ "${1:-} ${2:-}" = "machine create" ]; then
  exchange=''
  command=()
  after_separator=0
  shift 2
  while [ "$#" -gt 0 ]; do
    if [ "${after_separator}" -eq 1 ]; then
      command+=("$1")
    elif [ "$1" = "--" ]; then
      after_separator=1
    elif [ "$1" = "--volume" ]; then
      shift
      case "$1" in *:/exchange) exchange="${1%:/exchange}" ;; esac
    fi
    shift
  done
  printf '%s\n' "${exchange}" > "${FAKE_SMOLVM_STATE}"
  if [ "${#command[@]}" -ne 1 ] || [ "${command[0]:-}" != "/sbin/init" ]; then
    printf 'system machine persistent workload must be /sbin/init, got: %s\n' "${command[*]:-<none>}" >&2
    exit 42
  fi
  exit 0
fi
if [ "${1:-} ${2:-}" = "machine start" ]; then
  exchange="$(cat "${FAKE_SMOLVM_STATE}")"
  if [ "${FAKE_SMOLVM_RESULT}" != missing ]; then
    printf '%s\n' "${FAKE_SMOLVM_RESULT}" > "${exchange}/result"
  fi
  exit 0
fi
if [ "${1:-} ${2:-}" = "machine exec" ]; then
  echo 'systemd must not be launched through machine exec' >&2
  exit 43
fi
if [ "${1:-} ${2:-}" = "machine stop" ] || [ "${1:-} ${2:-}" = "machine delete" ]; then
  exit 0
fi
echo "unexpected fake smolvm command: $*" >&2
exit 91
`)
	return h
}

func (h *fakeSmolvmHarness) run(result, timeout string) (string, string, error) {
	h.t.Helper()
	artifactDir := h.t.TempDir()
	homeDir := h.t.TempDir()
	logPath := filepath.Join(h.stateDir, "smolvm-"+result+".log")
	statePath := filepath.Join(h.stateDir, "exchange-"+result)
	cmd := exec.Command("bash", filepath.Join(h.repoRoot, "scripts", "ci", "vm-run.sh"), "--image", "system", "--job", "privilege-boundary")
	cmd.Dir = h.repoRoot
	cmd.Env = append(os.Environ(),
		"PATH="+h.fakeBin+":"+os.Getenv("PATH"),
		"HOME="+homeDir,
		"CI_REPO_ROOT="+h.repoRoot,
		"CI_ARTIFACT_DIR="+artifactDir,
		"CI_TREEISH=HEAD",
		"CI_BACKEND=smolvm",
		"CI_CLEAN=1",
		"CI_CPUS=1",
		"CI_MEMORY=1",
		"CI_VM_TIMEOUT="+timeout,
		"FAKE_SMOLVM_LOG="+logPath,
		"FAKE_SMOLVM_STATE="+statePath,
		"FAKE_SMOLVM_RESULT="+result,
	)
	output, runErr := cmd.CombinedOutput()
	logBody, readErr := os.ReadFile(logPath)
	if readErr != nil {
		h.t.Fatalf("read fake smolvm log: %v", readErr)
	}
	return string(output), string(logBody), runErr
}

func TestLocalCISystemMachineBootsSystemdAsPersistentWorkload(t *testing.T) {
	output, log, err := newFakeSmolvmHarness(t).run("0", "5")
	if err != nil {
		t.Fatalf("system smolvm orchestration failed: %v\n%s", err, output)
	}
	for _, want := range []string{"machine create", "/sbin/init", "machine start", "machine stop", "machine delete"} {
		if !strings.Contains(log, want) {
			t.Errorf("smolvm lifecycle log missing %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "machine exec") {
		t.Fatalf("systemd was launched through machine exec instead of as the persistent PID-1 workload:\n%s", log)
	}
	createAt := strings.Index(log, "machine create")
	startAt := strings.Index(log, "machine start")
	stopAt := strings.Index(log, "machine stop")
	deleteAt := strings.Index(log, "machine delete")
	if createAt < 0 || startAt <= createAt || stopAt <= startAt || deleteAt <= stopAt {
		t.Fatalf("unexpected smolvm lifecycle order:\n%s", log)
	}
}

func TestLocalCISystemMachineFailsWhenSystemdPublishesNoResult(t *testing.T) {
	output, log, err := newFakeSmolvmHarness(t).run("missing", "1")
	if err == nil {
		t.Fatalf("system smolvm orchestration unexpectedly passed without a guest result:\n%s", output)
	}
	if !strings.Contains(output, "did not publish a result within 1s") {
		t.Fatalf("missing bounded-timeout diagnostic:\n%s", output)
	}
	for _, want := range []string{"machine stop", "machine delete"} {
		if !strings.Contains(log, want) {
			t.Errorf("timeout cleanup missing %q:\n%s", want, log)
		}
	}
}

func TestLocalCISystemMachinePropagatesGuestFailure(t *testing.T) {
	output, log, err := newFakeSmolvmHarness(t).run("23", "5")
	if err == nil {
		t.Fatalf("system smolvm orchestration masked guest failure:\n%s", output)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
		t.Fatalf("guest exit 23 was not propagated, got %v:\n%s", err, output)
	}
	for _, want := range []string{"machine stop", "machine delete"} {
		if !strings.Contains(log, want) {
			t.Errorf("failure cleanup missing %q:\n%s", want, log)
		}
	}
}
