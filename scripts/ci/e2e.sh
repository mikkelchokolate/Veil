#!/usr/bin/env bash
# scripts/ci/e2e.sh — real protocol E2E CI job.
# Hysteria2, Mieru TCP/UDP, NaiveProxy and olcRTC through panel-generated
# configuration, using REAL pinned runtimes (never symlinks to veil, never fake
# wrappers). olcRTC additionally runs the exact pinned upstream's self-contained
# in-memory data-path test so required CI covers its tunnel stack without relying
# on a public Jitsi/Telemost/WBStream service.
# Mirrors the former `e2e` workflow job; release runtimes come from
# /opt/veil-runtime when image-provisioned and fall back to pinned installers.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

if [ "$(id -u)" -eq 0 ] && ! id veil >/dev/null 2>&1; then
  useradd --system --user-group --no-create-home --shell /usr/sbin/nologin veil
fi

if [ "$(id -u)" -eq 0 ]; then SUDO=""; else SUDO="sudo"; fi

ci_step "web/dist (embedded into the binary)"
bash "${CI_SCRIPTS_DIR}/prepare-frontend-dist.sh"

ci_step "build and install Veil"
go build -o /tmp/veil-e2e ./cmd/veil
${SUDO} install -m 0755 /tmp/veil-e2e /usr/local/bin/veil

ci_step "protocol runtimes (pinned and verified — never /releases/latest)"
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

# olcRTC is source-built and its immutable commit/integrity policy lives in the
# product runtime descriptor. Provision it through Veil itself instead of
# duplicating that pin in this shell script. Pre-provisioned CI images may carry
# it in runtime_dir; older images transparently use the product installer here.
if [ -x "${runtime_dir}/olcrtc" ]; then
  ${SUDO} install -m 0755 "${runtime_dir}/olcrtc" /usr/local/bin/olcrtc
else
  olcrtc_runtime_dir="$(mktemp -d /tmp/veil-olcrtc-runtime.XXXXXX)"
  /usr/local/bin/veil runtime install --only olcrtc --bin-dir "${olcrtc_runtime_dir}"
  test -x "${olcrtc_runtime_dir}/olcrtc"
  ${SUDO} install -m 0755 "${olcrtc_runtime_dir}/olcrtc" /usr/local/bin/olcrtc
fi

/usr/local/bin/caddy list-modules | grep -Fx http.handlers.forward_proxy
command -v hysteria caddy mita mieru naive olcrtc

ci_step "assert protocol binaries are real (not veil shims)"
for bin in caddy naive mita mieru hysteria olcrtc; do
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

# The real olcRTC process needs an external conferencing provider for its normal
# srv/cnc session. Required CI must remain deterministic, so after verifying
# Veil's generated config with the real binary we resolve the exact module
# version embedded in that binary and run upstream's in-memory local tunnel E2E
# at that same version. This covers the pinned data path without substituting a
# fake Veil runtime or relying on public provider availability.
run_olcrtc_upstream_local() {
  local module_version module_json module_dir
  module_version="$(go version -m /usr/local/bin/olcrtc | awk '$1 == "mod" && $2 == "github.com/openlibrecommunity/olcrtc" { print $3; exit }')"
  if [ -z "${module_version}" ]; then
    echo "unable to resolve olcRTC module version from installed binary" >&2
    return 1
  fi
  module_json="$(go mod download -json "github.com/openlibrecommunity/olcrtc@${module_version}")"
  module_dir="$(printf '%s\n' "${module_json}" | awk -F '"' '/"Dir":/ { print $4; exit }')"
  if [ -z "${module_dir}" ] || [ ! -d "${module_dir}" ]; then
    echo "unable to resolve olcRTC module directory for ${module_version}" >&2
    return 1
  fi
  (
    cd "${module_dir}"
    go test ./internal/e2e \
      -run '^TestLocalThroughputSoak$' \
      -count=1 -v -timeout=45s \
      -args \
      -olcrtc.local-soak \
      -olcrtc.local-soak-duration=1s \
      -olcrtc.local-soak-transport=datachannel \
      -olcrtc.local-soak-chunk=16384 \
      -olcrtc.local-soak-progress=0s
  )
}

ci_run e2e-hysteria2 run_proto hysteria2 TestRequiredHysteria2DataPath
ci_run e2e-mieru-tcp run_proto mieru-tcp TestRequiredMieruTCPDataPath
ci_run e2e-mieru-udp run_proto mieru-udp TestRequiredMieruUDPDataPath
ci_run e2e-naiveproxy run_proto naiveproxy TestRequiredNaiveProxyDataPath
ci_run e2e-olcrtc-contract run_proto olcrtc TestRequiredOlcRTCRuntimeContract
ci_run e2e-olcrtc-local-data-path run_olcrtc_upstream_local

ci_log "e2e job passed"