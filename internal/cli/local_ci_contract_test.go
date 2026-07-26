package cli

import (
	"os"
	"strings"
	"testing"
)

func TestLocalCISmolvmAndDockerBoundaryContract(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(body), "\r\n", "\n")
	}
	runJob := read("../../scripts/ci/run-job.sh")
	for _, want := range []string{
		"CI_BACKEND=docker CI_FULL_PHASE=docker",
		"CI_BACKEND=docker \"${CI_SCRIPTS_DIR}/vm-run.sh\" --image system --job image-build",
		`[ "${JOB}" = "package-smoke" ]`,
	} {
		if !strings.Contains(runJob, want) {
			t.Errorf("run-job.sh missing host-Docker boundary %q", want)
		}
	}
	full := read("../../scripts/ci/full.sh")
	systemStart := strings.Index(full, "  system)")
	dockerStart := strings.Index(full, "  docker)")
	if systemStart < 0 || dockerStart <= systemStart {
		t.Fatal("full.sh phases missing")
	}
	systemPhase := full[systemStart:dockerStart]
	if strings.Contains(systemPhase, "package-smoke") || strings.Contains(systemPhase, "image-build") {
		t.Fatal("Docker-dependent job is still advertised in smolvm system phase")
	}
	if strings.Contains(full, "  all)") {
		t.Fatal("full.sh retains a mixed-backend all phase")
	}
	vmRun := read("../../scripts/ci/vm-run.sh")
	guestRun := read("../../ci/vm/guest-run.sh")
	if !strings.Contains(guestRun, `system|docker) JOB_USER="root"`) {
		t.Fatal("guest-run does not run explicit Docker full phase as root")
	}
	if !strings.Contains(vmRun, "docker export") || !strings.Contains(vmRun, ".veil-ci-rootfs-complete") {
		t.Fatal("smolvm path does not use content-keyed expanded rootfs handoff")
	}
	if strings.Contains(vmRun, "docker save") {
		t.Fatal("smolvm path retains unsupported guest-side docker-save archive extraction")
	}
	for _, want := range []string{"smolvm machine create", "smolvm machine start", "-- /sbin/init", "systemd-run-request", "smolvm machine exec", "smolvm machine stop", "smolvm machine delete"} {
		if !strings.Contains(vmRun, want) {
			t.Errorf("vm-run.sh missing systemd lifecycle %q", want)
		}
	}
	runner := read("../../ci/vm/systemd/run-job.sh")
	unit := read("../../ci/vm/systemd/veil-ci-runner.service")
	for _, want := range []string{"/opt/ci/systemd/poc.sh", "/opt/ci/guest-run.sh", "systemctl --no-block poweroff", `${exchange}/result`} {
		if !strings.Contains(runner, want) {
			t.Errorf("systemd runner missing %q", want)
		}
	}
	if !strings.Contains(unit, "ConditionPathExists=/exchange/systemd-run-request") {
		t.Fatal("systemd runner can start outside an explicit smolvm request")
	}
}

func TestLocalCIPrerequisiteContract(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	preflight := read("../../scripts/ci/vm-preflight.sh")
	for _, want := range []string{"CI_SMOLVM_MIN_VERSION", "sort -V", "amd64/x86_64 only", "Docker daemon is required"} {
		if !strings.Contains(preflight, want) {
			t.Errorf("preflight missing %q", want)
		}
	}
	browserScript := read("../../scripts/ci/browser-e2e.sh")
	if strings.Contains(browserScript, "${SUDO} -u") {
		t.Fatal("browser E2E root path tries to execute -u when SUDO is empty")
	}
	for _, want := range []string{"runuser -u veil --", "sudo -u veil --", "CI_IN_GUEST", "production Veil sentinels"} {
		if !strings.Contains(browserScript, want) {
			t.Fatalf("browser E2E user switch is missing %q", want)
		}
	}

	workflow := read("../../.github/workflows/ci.yml")
	if strings.Contains(workflow, "smolvm-default-backend") {
		t.Fatal("normal GitHub CI duplicates the complete local smolvm suite")
	}
	ciDocs := read("../../docs/development/ci.md")
	for _, want := range []string{"not proof of create/start/systemd/exec/stop", "make ci-full", "exact HEAD"} {
		if !strings.Contains(ciDocs, want) {
			t.Errorf("CI docs missing smolvm disclosure %q", want)
		}
	}

	dirtyEscape := "CI_" + "ALLOW_DIRTY"
	if strings.Contains(read("../../scripts/ci/snapshot.sh"), dirtyEscape) {
		t.Fatal("undeclared dirty-tree escape remains")
	}
	if strings.Contains(read("../../scripts/ci/vm-build.sh"), "bc -l") {
		t.Fatal("vm-build has undeclared bc dependency")
	}
}
