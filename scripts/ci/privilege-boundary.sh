#!/usr/bin/env bash
# scripts/ci/privilege-boundary.sh — privilege boundary CI job.
# systemd socket activation + helper socket + filesystem access matrix (root),
# hardened unit validation. Mirrors the former `privilege-boundary` workflow job.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

ci_step "web/dist (embedded into the binary)"
bash "${CI_SCRIPTS_DIR}/prepare-frontend-dist.sh"

ci_step "systemd socket activation (linuxintegration)"
ci_run privilege-socket-activation \
  go test -tags linuxintegration ./internal/privileged -run TestSystemdSocketActivationAdoptsFD3 -count=1 -v
ci_assert_tests_ran "${CI_ARTIFACT_DIR}/privilege-socket-activation.log"

ci_step "helper socket and filesystem access matrix (root)"
if [ "$(id -u)" -eq 0 ]; then SUDO=""; else SUDO="sudo"; fi
if ! getent group veil >/dev/null; then
  ${SUDO} groupadd --system veil
fi
if ! getent passwd veil >/dev/null; then
  ${SUDO} useradd --system --gid veil --home-dir /nonexistent --shell /usr/sbin/nologin veil
fi
if ! getent group veil-proxy >/dev/null; then
  ${SUDO} groupadd --system veil-proxy
fi
if ! getent passwd veil-proxy >/dev/null; then
  ${SUDO} useradd --system --gid veil-proxy --home-dir /nonexistent --shell /usr/sbin/nologin veil-proxy
fi
# The supplementary group membership is load-bearing: the linuxintegration DAC
# and proxy-access contracts assume veil∈veil-proxy. A failed usermod must fail
# the job — soft-failing would leave those assumptions vacuously weak (#410).
${SUDO} usermod -aG veil-proxy veil
id veil | grep -qw veil-proxy || ci_die "veil is not a member of veil-proxy"
if [ "$(id -u)" -eq 0 ]; then
  ci_run privilege-access-matrix \
    go test -tags linuxintegration ./test/linuxintegration/... -count=1 -v
else
  ci_run privilege-access-matrix \
    sudo env "PATH=${PATH}" "HOME=${HOME}" go test -tags linuxintegration ./test/linuxintegration/... -count=1 -v
fi
ci_assert_tests_ran "${CI_ARTIFACT_DIR}/privilege-access-matrix.log"
# A suite where every test skips still satisfies ci_assert_tests_ran — require
# at least one real pass, and require the security-contract roots by name so a
# missing veil/nobody account cannot skip the DAC/permission evidence (#411, #428).
ci_assert_tests_passed "${CI_ARTIFACT_DIR}/privilege-access-matrix.log"
ci_assert_test_passed "${CI_ARTIFACT_DIR}/privilege-access-matrix.log" TestBackupServiceHardeningCanReadVeilOwnedState
ci_assert_test_passed "${CI_ARTIFACT_DIR}/privilege-access-matrix.log" TestIntegrationPanelPermissionMatrix

ci_step "hardened systemd units"
go build -o /tmp/veil-unit-verify ./cmd/veil
${SUDO} install -m 0755 /tmp/veil-unit-verify /usr/local/bin/veil
for bin in caddy hysteria mita olcrtc sing-box; do
  ${SUDO} ln -sf /usr/local/bin/veil "/usr/local/bin/${bin}"
done
# systemd-analyze verify is a STATIC check only — the real lifecycle proof is
# the socket-activation test above plus the systemd PoC in ci/vm/systemd/.
systemd-analyze verify packaging/systemd/*.service packaging/systemd/*.socket packaging/systemd/*.timer

ci_step "packaged unit hardening contract"
# Static guard rail: the shipped units must carry the privilege-boundary
# contract, not just render it under test. Fail loudly if a unit regresses.
assert_unit() { # unit regex
  grep -Eq "$2" "packaging/systemd/$1" || {
    echo "unit contract violation: $1 missing /$2/" >&2
    exit 1
  }
}
assert_unit veil.service '^User=veil$'
assert_unit veil.service '^Group=veil$'
for unit in veil-caddy.service veil-hysteria2@.service veil-olcrtc@.service veil-warp.service veil-mieru.service; do
  assert_unit "$unit" '^User=veil-proxy$'
  assert_unit "$unit" '^Group=veil-proxy$'
  assert_unit "$unit" '^InaccessiblePaths=.*[[:space:]/]run/veil/helper\.sock'
  assert_unit "$unit" '^InaccessiblePaths=.*[[:space:]/]var/lib/veil'
done
assert_unit veil-helper.socket '^SocketUser=root$'
assert_unit veil-helper.socket '^SocketGroup=veil$'
assert_unit veil-helper.socket '^SocketMode=0660$'
assert_unit veil-helper.socket '^DirectoryMode=0711$'
assert_unit veil-helper.socket '^RemoveOnStop=true$'
assert_unit veil-helper.service '^User=root$'
# The helper must not be able to write the runtime dir holding its own socket.
if grep -Eq '^ReadWritePaths=.*[[:space:]](/run|/var/run)([[:space:]]|$)' packaging/systemd/veil-helper.service; then
  echo "veil-helper.service still has a writable /run path" >&2
  exit 1
fi

ci_log "privilege-boundary job passed"
