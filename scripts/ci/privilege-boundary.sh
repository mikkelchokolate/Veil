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
(cd web && pnpm install --frozen-lockfile && pnpm build)

ci_step "systemd socket activation (linuxintegration)"
go test -tags linuxintegration ./internal/privileged -run TestSystemdSocketActivationAdoptsFD3 -count=1 -v

ci_step "helper socket and filesystem access matrix (root)"
if [ "$(id -u)" -eq 0 ]; then SUDO=""; else SUDO="sudo"; fi
if ! getent group veil >/dev/null; then
  ${SUDO} groupadd --system veil
fi
if ! getent passwd veil >/dev/null; then
  ${SUDO} useradd --system --gid veil --home-dir /nonexistent --shell /usr/sbin/nologin veil
fi
if [ "$(id -u)" -eq 0 ]; then
  go test -tags linuxintegration ./test/linuxintegration/... -count=1 -v
else
  sudo env "PATH=${PATH}" "HOME=${HOME}" go test -tags linuxintegration ./test/linuxintegration/... -count=1 -v
fi

ci_step "hardened systemd units"
go build -o /tmp/veil-unit-verify ./cmd/veil
${SUDO} install -m 0755 /tmp/veil-unit-verify /usr/local/bin/veil
for bin in caddy hysteria mita olcrtc sing-box; do
  ${SUDO} ln -sf /usr/local/bin/veil "/usr/local/bin/${bin}"
done
# systemd-analyze verify is a STATIC check only — the real lifecycle proof is
# the socket-activation test above plus the systemd PoC in ci/vm/systemd/.
systemd-analyze verify packaging/systemd/*.service packaging/systemd/*.socket packaging/systemd/*.timer

ci_log "privilege-boundary job passed"
