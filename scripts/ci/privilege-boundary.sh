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
# A SKIP still satisfies ci_assert_tests_ran — require the PASS by name so a
# soft-skipped activation probe cannot green the job (issue #685).
ci_assert_test_passed "${CI_ARTIFACT_DIR}/privilege-socket-activation.log" TestSystemdSocketActivationAdoptsFD3

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
# veil-mita is the dedicated veil-mieru.service identity: the appctl socket is
# group veil-mita so the panel — and nothing else — can connect (issue #624).
if ! getent group veil-mita >/dev/null; then
  ${SUDO} groupadd --system veil-mita
fi
if ! getent passwd veil-mita >/dev/null; then
  ${SUDO} useradd --system --gid veil-mita --home-dir /nonexistent --shell /usr/sbin/nologin veil-mita
fi
# The supplementary group membership is load-bearing: the linuxintegration DAC
# and proxy-access contracts assume veil∈veil-proxy. A failed usermod must fail
# the job — soft-failing would leave those assumptions vacuously weak (#410).
${SUDO} usermod -aG veil-proxy veil
id veil | grep -qw veil-proxy || ci_die "veil is not a member of veil-proxy"
# Same contract for the appctl boundary: the panel must be a veil-mita member,
# and the edge account must NOT be — otherwise the socket ACL is meaningless.
${SUDO} usermod -aG veil-mita veil
id veil | grep -qw veil-mita || ci_die "veil is not a member of veil-mita"
if id -nG veil-proxy 2>/dev/null | tr ' ' '\n' | grep -qx veil-mita; then
  ci_die "veil-proxy must not be a member of veil-mita"
fi
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
# missing veil/nobody account cannot skip the DAC/permission evidence
# (#411, #428). The helper-socket and key-rotation roots are security evidence
# too — a soft-skip there must fail the job, not green it (issue #685).
ci_assert_tests_passed "${CI_ARTIFACT_DIR}/privilege-access-matrix.log"
for required_root in \
    TestBackupServiceHardeningCanReadVeilOwnedState \
    TestIntegrationPanelPermissionMatrix \
    TestIntegrationHelperSocketCanonicalLayout \
    TestIntegrationHelperSocketRejectsProxyUID \
    TestIntegrationHelperSocketAcceptsAllowedUIDAndDispatches \
    TestIntegrationPrivilegedKeyRotationRecoveryAcrossDurablePhases; do
  ci_assert_test_passed "${CI_ARTIFACT_DIR}/privilege-access-matrix.log" "${required_root}"
done

ci_step "runtime version-probe sandbox isolation (root + systemd)"
# The runtimeinstall sandbox-integration roots require root AND a live systemd
# manager, so the unprivileged `test` job always soft-skips them (allowlisted).
# This job is their only execution evidence — assert each root by name so a
# silently skipped suite fails the gate instead of greening it (issue #632).
# The in-sandbox helper root (TestRuntimeProbeMaliciousHelper) is spawned by
# the parent inside systemd-run; it stays allowlisted for the `test` job only.
[ -S /run/systemd/private ] || ci_die "systemd manager socket missing — probe sandbox roots cannot execute here"
if [ "$(id -u)" -eq 0 ]; then
  ci_run privilege-runtime-probe-sandbox \
    go test ./internal/runtimeinstall \
    -run '^(TestRuntimeVersionProbeExecutesProtectedHomeBinaryThroughSystemdBind|TestMaliciousRuntimeProbeCannotReadHostSecretsEnvironmentDescriptorsOrNetwork)$' \
    -count=1 -v
else
  ci_run privilege-runtime-probe-sandbox \
    sudo env "PATH=${PATH}" "HOME=${HOME}" go test ./internal/runtimeinstall \
    -run '^(TestRuntimeVersionProbeExecutesProtectedHomeBinaryThroughSystemdBind|TestMaliciousRuntimeProbeCannotReadHostSecretsEnvironmentDescriptorsOrNetwork)$' \
    -count=1 -v
fi
ci_assert_tests_ran "${CI_ARTIFACT_DIR}/privilege-runtime-probe-sandbox.log"
ci_assert_test_passed "${CI_ARTIFACT_DIR}/privilege-runtime-probe-sandbox.log" TestRuntimeVersionProbeExecutesProtectedHomeBinaryThroughSystemdBind
ci_assert_test_passed "${CI_ARTIFACT_DIR}/privilege-runtime-probe-sandbox.log" TestMaliciousRuntimeProbeCannotReadHostSecretsEnvironmentDescriptorsOrNetwork

ci_step "custom --etc-dir/--var-dir install contract (issue #664)"
# Every leg above exercises only the packaged /etc/veil + /var/lib/veil
# layout. The custom-root contract — drop-in ReadWritePaths/InaccessiblePaths
# resets, the backup ConditionPathExists re-point, helper path grants, custom
# root propagation, ownership, the fixed /var/lib/{mita,caddy} state dirs,
# and the var-lib fallback migration — is enforced by named unit tests;
# assert each root by name so a soft-skip cannot green the job. This leg must
# run BEFORE the "hardened systemd units" step symlinks /usr/local/bin/caddy
# to the veil binary — the ownership test probes real `caddy list-modules`
# and a veil symlink fails closed.
if [ "$(id -u)" -eq 0 ]; then
  ci_run privilege-custom-roots \
    go test ./internal/renderer ./internal/installer ./internal/cliflow/status ./internal/cli ./internal/managementstate -count=1 -v \
    -run '^(TestRenderInstallDropInOverridesPackagedUnits|TestInstallApplyPropagatesCustomEtcAndVarDir|TestApplyRURecommendedProfileChownsSecretsForVeilGroup|TestStatusDiscoversEnvFileUnderCustomEtcDir|TestUninstallCaddyMitaStateDirOverrides|TestDecodeV4MigratesVarLibFallbackRoots)$'
else
  ci_run privilege-custom-roots \
    sudo env "PATH=${PATH}" "HOME=${HOME}" \
    go test ./internal/renderer ./internal/installer ./internal/cliflow/status ./internal/cli ./internal/managementstate -count=1 -v \
    -run '^(TestRenderInstallDropInOverridesPackagedUnits|TestInstallApplyPropagatesCustomEtcAndVarDir|TestApplyRURecommendedProfileChownsSecretsForVeilGroup|TestStatusDiscoversEnvFileUnderCustomEtcDir|TestUninstallCaddyMitaStateDirOverrides|TestDecodeV4MigratesVarLibFallbackRoots)$'
fi
ci_assert_tests_ran "${CI_ARTIFACT_DIR}/privilege-custom-roots.log"
for required_root in \
    TestRenderInstallDropInOverridesPackagedUnits \
    TestInstallApplyPropagatesCustomEtcAndVarDir \
    TestApplyRURecommendedProfileChownsSecretsForVeilGroup \
    TestStatusDiscoversEnvFileUnderCustomEtcDir \
    TestUninstallCaddyMitaStateDirOverrides \
    TestDecodeV4MigratesVarLibFallbackRoots; do
  ci_assert_test_passed "${CI_ARTIFACT_DIR}/privilege-custom-roots.log" "${required_root}"
done

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
for unit in veil-caddy.service veil-hysteria2@.service veil-olcrtc@.service veil-warp.service; do
  assert_unit "$unit" '^User=veil-proxy$'
  assert_unit "$unit" '^Group=veil-proxy$'
  assert_unit "$unit" '^InaccessiblePaths=.*[[:space:]/]run/veil/helper\.sock'
  assert_unit "$unit" '^InaccessiblePaths=.*[[:space:]/]var/lib/veil'
done
# veil-mieru.service is deliberately NOT a veil-proxy unit (issue #624): the
# dedicated veil-mita identity owns the appctl socket and state dir, keeps
# veil-proxy only as a supplementary group for reading generated config, and
# locks the socket directory down so edge peers cannot even traverse it.
assert_unit veil-mieru.service '^User=veil-mita$'
assert_unit veil-mieru.service '^Group=veil-mita$'
assert_unit veil-mieru.service '^SupplementaryGroups=.*veil-proxy'
assert_unit veil-mieru.service '^RuntimeDirectory=veil-mieru$'
assert_unit veil-mieru.service '^RuntimeDirectoryMode=0750$'
assert_unit veil-mieru.service '^UMask=0007$'
assert_unit veil-mieru.service '^InaccessiblePaths=.*[[:space:]/]run/veil/helper\.sock'
assert_unit veil-mieru.service '^InaccessiblePaths=.*[[:space:]/]var/lib/veil'
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

ci_step "mita appctl socket isolation probe (issue #624)"
# Functional evidence for the shipped contract — RuntimeDirectory
# veil-mita:veil-mita 0750 plus a UMask 0007 socket (0770 veil-mita): a
# veil-proxy edge peer must get EACCES while the panel (supplementary
# veil-mita member) passes the permission check. ECONNREFUSED means "perms
# OK, nothing listening" — for the positive probe that IS the pass signal.
run_as() { # <user> <cmd...>
  if [ "$(id -u)" -eq 0 ]; then
    runuser -u "$1" -- "${@:2}"
  else
    sudo -u "$1" -- "${@:2}"
  fi
}
command -v python3 >/dev/null || ci_die "python3 is required for the appctl boundary probe"
probe_dir="/run/veil-mita-probe"
probe_sock="${probe_dir}/mita.sock"
${SUDO} rm -rf "${probe_dir}"
${SUDO} install -d -m 0750 -o veil-mita -g veil-mita "${probe_dir}"
# Bind the node under the daemon identity with the unit's umask — the same
# 0770 veil-mita node the real mita creates.
run_as veil-mita python3 - "${probe_sock}" <<'PY'
import os, socket, sys
old = os.umask(0o007)
try:
    s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    s.bind(sys.argv[1])
    s.close()
finally:
    os.umask(old)
PY

# Edge peer: only PermissionError counts as evidence — any other outcome
# (including ECONNREFUSED, which means the permission check PASSED) fails.
# `cmd || rc=$?` (not `if !`) so the real probe exit code reaches the report.
probe_rc=0
run_as veil-proxy python3 - "${probe_sock}" <<'PY' || probe_rc=$?
import socket, sys
try:
    socket.socket(socket.AF_UNIX, socket.SOCK_STREAM).connect(sys.argv[1])
except PermissionError:
    sys.exit(0)   # EACCES — the boundary held
except OSError:
    sys.exit(2)   # unexpected result — broken probe, not evidence
sys.exit(1)       # connected — the boundary failed
PY
if [ "${probe_rc}" -ne 0 ]; then
  ${SUDO} rm -rf "${probe_dir}"
  ci_die "veil-proxy peer was not EACCES-denied on the mita appctl contract (probe rc=${probe_rc})"
fi

# Panel: EACCES means the panel lost its own control channel — a regression.
probe_rc=0
run_as veil python3 - "${probe_sock}" <<'PY' || probe_rc=$?
import socket, sys
try:
    socket.socket(socket.AF_UNIX, socket.SOCK_STREAM).connect(sys.argv[1])
except PermissionError:
    sys.exit(1)   # EACCES — panel locked out of appctl
except OSError:
    sys.exit(0)   # permission check passed (nothing listens in this probe)
sys.exit(0)
PY
if [ "${probe_rc}" -ne 0 ]; then
  ${SUDO} rm -rf "${probe_dir}"
  ci_die "panel account could not pass the mita appctl socket permission check (probe rc=${probe_rc})"
fi
${SUDO} rm -rf "${probe_dir}"
ci_log "appctl boundary: veil-proxy peer denied, panel allowed"

ci_log "privilege-boundary job passed"
