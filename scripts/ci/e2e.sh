#!/usr/bin/env bash
# scripts/ci/e2e.sh — real protocol E2E CI job.
# Hysteria2, Mieru TCP/UDP, NaiveProxy through panel-generated configuration,
# using REAL pinned runtimes (never symlinks to veil, never fake wrappers).
# Mirrors the former `e2e` workflow job; runtime installation is pinned:
# binaries come from /opt/veil-runtime (image-provisioned, checksum-verified)
# instead of resolving /releases/latest at test time.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

if [ "$(id -u)" -eq 0 ]; then SUDO=""; else SUDO="sudo"; fi

ci_step "web/dist (embedded into the binary)"
(cd web && pnpm install --frozen-lockfile && pnpm build)

ci_step "build and install Veil"
go build -o /tmp/veil-e2e ./cmd/veil
${SUDO} install -m 0755 /tmp/veil-e2e /usr/local/bin/veil

ci_step "protocol runtimes (pinned, checksum-verified — never /releases/latest)"
runtime_dir="${VEIL_CI_RUNTIME_DIR:-/opt/veil-runtime}"
if [ ! -x "${runtime_dir}/hysteria" ]; then
  runtime_dir="$(mktemp -d /tmp/veil-runtime.XXXXXX)"
  # shellcheck source=scripts/ci/runtimes.sh
  . "${CI_SCRIPTS_DIR}/runtimes.sh"
  install_pinned_runtimes "${runtime_dir}" "/usr/local/bin/veil"
fi
${SUDO} install -m 0755 "${runtime_dir}/hysteria" /usr/local/bin/hysteria
${SUDO} install -m 0755 "${runtime_dir}/mita" /usr/local/bin/mita
${SUDO} install -m 0755 "${runtime_dir}/caddy" /usr/local/bin/caddy
${SUDO} install -m 0755 "${runtime_dir}/mieru" /usr/local/bin/mieru
${SUDO} install -m 0755 "${runtime_dir}/naive" /usr/local/bin/naive
/usr/local/bin/caddy list-modules | grep -Fx http.handlers.forward_proxy
command -v hysteria caddy mita mieru naive

ci_step "assert protocol binaries are real (not veil shims)"
for bin in caddy naive mita mieru hysteria; do
  command -v "${bin}" >/dev/null
  [ "$(readlink -f "$(command -v "${bin}")")" != /usr/local/bin/veil ]
done

run_proto() {
  local name="$1" testname="$2"
  set -o pipefail
  go test -tags e2e ./test/e2e/... -run "^${testname}\$" -count=1 -v -timeout=90s 2>&1 | tee "${CI_ARTIFACT_DIR}/e2e-${name}.log"
  local rc=${PIPESTATUS[0]}
  set +o pipefail
  return "${rc}"
}

ci_run e2e-hysteria2 run_proto hysteria2 TestRequiredHysteria2DataPath
ci_run e2e-mieru-tcp run_proto mieru-tcp TestRequiredMieruTCPDataPath
ci_run e2e-mieru-udp run_proto mieru-udp TestRequiredMieruUDPDataPath
ci_run e2e-naiveproxy run_proto naiveproxy TestRequiredNaiveProxyDataPath

ci_log "e2e job passed"
