#!/usr/bin/env bash
# scripts/ci/install-acceptance.sh — install-acceptance CI job (audit #304).
#
# Exercises the REAL `veil install` contract on a live systemd host — the gap
# between unit tests and `veil serve`-only e2e:
#   1. `veil install --check` prints the capability report and mutates nothing;
#   2. `veil install` provisions prerequisites, writes config/state, renders
#      units, and starts services;
#   3. systemd units, helper socket, state files, firewall rules, and panel
#      readiness are verified;
#   4. a second install proves reinstall idempotency (existing state reused);
#   5. a controlled ACME CA (pebble) issues a REAL certificate — no public
#      Let's Encrypt dependency (audit #304);
#   6. inside the smolvm system VM the guest REBOOTS and the runner re-enters
#      the job, which verifies unit/panel/state persistence across boot;
#   7. `veil uninstall --keep-data` stops services while preserving state.
#
# Requires systemd as PID 1 — the job maps to the `system` image locally and
# runs on the systemd-enabled ubuntu-24.04 runner in GitHub Actions.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

systemd_ready=1
for _ in $(seq 1 90); do
  state="$(systemctl is-system-running 2>/dev/null || true)"
  case "${state}" in running|degraded) systemd_ready=0; break ;; esac
  sleep 1
done
if [ "${systemd_ready}" -ne 0 ]; then
  ci_die "install-acceptance requires a live systemd init (pid1=$(cat /proc/1/comm), state=${state:-unknown})"
fi

if [ "$(id -u)" -eq 0 ]; then SUDO=""; else SUDO="sudo"; fi

# Failure forensics: capture the panel journal and unit states whenever the
# job dies mid-flight - the interesting failures are API/apply hangs.
collect_diagnostics() {
  rc=$?
  [ -d "${CI_ARTIFACT_DIR}" ] || return "${rc}"
  ${SUDO:-} journalctl --no-pager -n 400 -u veil.service -u veil-helper.service -u veil-helper.socket > "${CI_ARTIFACT_DIR}/veil-units-journal.txt" 2>&1 || true
  ${SUDO:-} journalctl --no-pager -n 200 -u "veil-hysteria2@*" -u "veil-mieru.service" -u "veil-olcrtc@*" -u veil-caddy.service > "${CI_ARTIFACT_DIR}/protocol-units-journal.txt" 2>&1 || true
  systemctl list-units --all "veil*" --no-pager > "${CI_ARTIFACT_DIR}/veil-units.txt" 2>&1 || true
  ps aux > "${CI_ARTIFACT_DIR}/processes.txt" 2>&1 || true
  return "${rc}"
}
trap collect_diagnostics EXIT

PANEL_PORT=2096
INSTALL_FLAGS=(--yes --panel-access direct --le-ip-cert=false --public-ip 127.0.0.1 --panel-port "${PANEL_PORT}")
# Custom-root leg (issue #746): the install left live through the smolvm
# reboot is this one, so phase B proves the custom trees persist.
CUSTOM_ROOT=/opt/veil-ci
CUSTOM_ETC="${CUSTOM_ROOT}/etc"
CUSTOM_VAR="${CUSTOM_ROOT}/var"

uninstall_leg() {
  ci_step "uninstall --keep-data"
  ci_run veil-uninstall ${SUDO} /usr/local/bin/veil uninstall --yes --keep-data
  # veil.service alone cannot prove teardown: the helper socket, the caddy
  # unit (when the caddy leg installed it) and every protocol unit started by
  # the first-inbound leg must also be down and disabled (#407).
  local unit
  for unit in veil.service veil-helper.service veil-helper.socket veil-caddy.service \
              veil-hysteria2@ci-hy2.service veil-mieru.service veil-olcrtc@ci-olc.service; do
    if systemctl is-active --quiet "${unit}"; then
      ci_die "${unit} still active after uninstall"
    fi
    if systemctl is-enabled --quiet "${unit}" 2>/dev/null; then
      ci_die "${unit} still enabled after uninstall"
    fi
  done
  ${SUDO} test -s /var/lib/veil/state.json || ci_die "--keep-data removed state.json"
  ${SUDO} journalctl --no-pager -u veil.service -u veil-helper.service -u veil-helper.socket \
    > "${CI_ARTIFACT_DIR}/veil-units-journal.txt" 2>&1 || true
}

# The protocol units rendered by the first-inbound leg must survive panel
# restarts, reinstalls, and reboots — assert them as a group so each leg
# re-proves the whole contract, not just veil.service. olcRTC needs an
# external conferencing provider at runtime, so its contract is the enabled
# instance rather than an active process.
assert_protocol_units() { # $1 = leg context for diagnostics
  systemctl is-active --quiet veil-hysteria2@ci-hy2.service \
    || ci_die "veil-hysteria2@ci-hy2 not active $1"
  systemctl is-active --quiet veil-mieru.service \
    || ci_die "veil-mieru not active $1"
  systemctl is-enabled --quiet veil-olcrtc@ci-olc.service \
    || ci_die "veil-olcrtc@ci-olc not enabled $1"
  # is-enabled alone can green a unit that failed to start (or was never
  # rendered): require the unit file to exist, the binary it executes to be
  # installed, and systemd to have reached ExecStart at least once (#408).
  systemctl cat veil-olcrtc@ci-olc.service >/dev/null 2>&1 \
    || ci_die "veil-olcrtc@ci-olc unit file missing $1"
  # ci-olc dials a deliberately dead room (127.0.0.1:1), so the unit
  # crash-loops and can hit StartLimitBurst exactly when a leg restart packs
  # two attempts together — terminal "failed" is a fixture property, not a
  # regression. The deterministic contract is that systemd spawned ExecStart
  # at least once (#408); ExecMainStartTimestamp survives later crashes.
  [ -n "$(${SUDO} systemctl show -P ExecMainStartTimestamp veil-olcrtc@ci-olc.service 2>/dev/null)" ] \
    || ci_die "veil-olcrtc@ci-olc never reached ExecStart $1"
  ${SUDO} test -x /usr/local/bin/olcrtc \
    || ci_die "olcrtc runtime binary missing/non-executable $1"
}

# probe_panel_healthz <etc-dir> — read the live install's token/base path from
# its env file and require /healthz to answer HTTP 200 with a "status":"ok"
# payload. A bare exit-0 or "status" key assert cannot prove health: an
# unhealthy panel still emits the field. Used by the first-install check, the
# post-reboot leg, and the custom-root leg (which reads the custom etc dir).
probe_panel_healthz() {
  local etc_dir="$1" token base_path code
  token="$(${SUDO} grep '^VEIL_API_TOKEN=' "${etc_dir}/veil.env" | cut -d= -f2- | tr -d '[:space:]')"
  base_path="$(${SUDO} grep '^VEIL_WEB_BASE_PATH=' "${etc_dir}/veil.env" | cut -d= -f2- | tr -d '[:space:]')"
  { [ -n "${token}" ] && [ -n "${base_path}" ]; } \
    || ci_die "${etc_dir}/veil.env lacks VEIL_API_TOKEN/VEIL_WEB_BASE_PATH"
  code="$(curl --http1.1 -sk --max-time 60 -o /tmp/ia-health.json -w '%{http_code}' \
    -H "X-Veil-Token: ${token}" "https://127.0.0.1:${PANEL_PORT}${base_path}healthz")"
  [ "${code}" = "200" ] || ci_die "panel /healthz returned HTTP ${code}"
  grep -q '"status":"ok"' /tmp/ia-health.json \
    || ci_die "panel /healthz did not report status ok: $(cat /tmp/ia-health.json)"
}

# Pinned pebble test CA. Always installed at CI_PEBBLE_VERSION — a stale
# preinstalled binary is not evidence (#454) — and patched to validate
# tls-alpn-01 against Caddy's real :443 listener. This is a function because
# phase B re-enters the job after a guest reboot: the CA process does not
# survive reboot and must be restarted (#405).
PEBBLE_MOD=""
PEBBLE_ISSUING_ROOT=""
start_pebble() {
  local pebble_bin
  pebble_bin="$(go env GOPATH)/bin/pebble"
  GOBIN="$(go env GOPATH)/bin" go install "github.com/letsencrypt/pebble/v2/cmd/pebble@${CI_PEBBLE_VERSION}"
  [ -x "${pebble_bin}" ] || ci_die "pinned pebble install produced no ${pebble_bin}"
  PEBBLE_MOD="$(go env GOMODCACHE)/github.com/letsencrypt/pebble/v2@${CI_PEBBLE_VERSION}"
  [ -f "${PEBBLE_MOD}/test/config/pebble-config.json" ] \
    || ci_die "pebble module cache has no test config at ${PEBBLE_MOD}"
  sed 's/"tlsPort": 5001/"tlsPort": 443/' "${PEBBLE_MOD}/test/config/pebble-config.json" \
    > "${CI_ARTIFACT_DIR}/pebble-config.json"
  (cd "${PEBBLE_MOD}" && PEBBLE_VA_NOSLEEP=1 "${pebble_bin}" -config "${CI_ARTIFACT_DIR}/pebble-config.json" \
    > "${CI_ARTIFACT_DIR}/pebble.log" 2>&1 &)
  for _ in $(seq 1 60); do
    if grep -q newOrder < <(curl --http1.1 -sk --max-time 60 https://127.0.0.1:14000/dir); then
      break
    fi
    sleep 1
  done
  grep -q newOrder < <(curl --http1.1 -sk --max-time 60 https://127.0.0.1:14000/dir) \
    || ci_die "pebble ACME directory did not come up (see pebble.log)"
  # The chain caddy serves is issued by the root pebble GENERATES at startup,
  # not by the static minica that signs pebble's API TLS certificate — fetch
  # the live issuing root from the management interface.
  PEBBLE_ISSUING_ROOT="${CI_ARTIFACT_DIR}/pebble-issuing-root.pem"
  curl -sf --cacert "${PEBBLE_MOD}/test/certs/pebble.minica.pem" \
    "https://127.0.0.1:15000/roots/0" -o "${PEBBLE_ISSUING_ROOT}" \
    || ci_die "could not fetch pebble issuing root from the management API"
}

# Caddy panel-proxy leg: real caddy-mode install, real tls-alpn-01 issuance
# for the panel domain against pebble, then an HTTPS request through the
# public :443 route that must return the panel SPA — HTTP 200 plus the
# rendered app shell, not merely "not a caddy fallback status" (#406).
# Scope honesty: this leg does not exercise naiveproxy; its data-path coverage
# is owned by the e2e job (TestRequiredNaiveProxyDataPath), see issue #351.
caddy_route_leg() {
  ci_step "caddy-mode install: real ACME issuance + public route"
  local domain="veil-ci.test"
  # Both the pebble VA (its system resolver honours /etc/hosts) and the local
  # route probe must resolve the panel domain to this host.
  grep -q " ${domain}" /etc/hosts || echo "127.0.0.1 ${domain}" | ${SUDO} tee -a /etc/hosts >/dev/null
  # Trust anchor for Caddy's ACME client; readable by the veil user through
  # the unit's ReadOnlyPaths=/etc/veil.
  ${SUDO} install -m 0644 "${PEBBLE_MOD}/test/certs/pebble.minica.pem" /etc/veil/acme-root.pem
  ci_run veil-install-caddy ${SUDO} env \
    VEIL_ACME_CA_URL="https://127.0.0.1:14000/dir" VEIL_ACME_CA_ROOT=/etc/veil/acme-root.pem \
    /usr/local/bin/veil install --yes --panel-access caddy --domain "${domain}" --email "ci@veil-ci.test"
  systemctl is-active --quiet veil-caddy.service || ci_die "veil-caddy.service is not active after caddy-mode install"
  systemctl is-enabled --quiet veil-caddy.service || ci_die "veil-caddy.service is not enabled"
  # Re-read the base path THIS install wrote — the probe must verify the URL
  # the panel actually serves now, not the value cached during the
  # direct-mode leg (reinstall reuses state today, but the contract is the
  # env on disk).
  local base_path
  base_path="$(${SUDO} grep '^VEIL_WEB_BASE_PATH=' /etc/veil/veil.env | cut -d= -f2- | tr -d '[:space:]' || true)"
  [ -n "${base_path}" ] || ci_die "VEIL_WEB_BASE_PATH missing from /etc/veil/veil.env after caddy-mode install"
  # tls-alpn-01 issuance completes on the first TLS handshake; poll the public
  # route until caddy serves a pebble-issued certificate and proxies the
  # panel. Only HTTP 200 proves the request reached the panel — 301/401/403/
  # 500/503 all previously greened a broken route (#406).
  local route_code=000
  for _ in $(seq 1 60); do
    route_code="$(curl --http1.1 -s --cacert "${PEBBLE_ISSUING_ROOT}" \
      --resolve "${domain}:443:127.0.0.1" --max-time 15 \
      -o /tmp/ia-caddy-route.out -w '%{http_code}' "https://${domain}${base_path}" 2>/tmp/ia-caddy-route.err || true)"
    [ "${route_code}" = "200" ] && break
    sleep 2
  done
  cp /tmp/ia-caddy-route.out "${CI_ARTIFACT_DIR}/caddy-route.out" 2>/dev/null || true
  cp /tmp/ia-caddy-route.err "${CI_ARTIFACT_DIR}/caddy-route.err" 2>/dev/null || true
  [ "${route_code}" = "200" ] \
    || ci_die "public caddy route https://${domain}${base_path} did not serve HTTP 200 (last ${route_code})"
  # A 200 could still be an error page — the route must serve the actual
  # panel SPA shell.
  grep -q 'id="root"' "${CI_ARTIFACT_DIR}/caddy-route.out" \
    || ci_die "public route returned 200 but not the panel SPA: $(head -c 400 "${CI_ARTIFACT_DIR}/caddy-route.out" 2>/dev/null)"
  # --cacert proved the chain verifies; also assert the served certificate's
  # issuer explicitly, at the same strength as the direct IP-cert leg.
  openssl s_client -connect "127.0.0.1:443" -servername "${domain}" </dev/null 2>/dev/null \
    | openssl x509 -noout -issuer | tee "${CI_ARTIFACT_DIR}/caddy-served-issuer.txt" || true
  grep -qi "pebble" "${CI_ARTIFACT_DIR}/caddy-served-issuer.txt" \
    || ci_die "public route is not serving a pebble-issued certificate: $(cat "${CI_ARTIFACT_DIR}/caddy-served-issuer.txt")"
}

# Reboot persistence (audit #304): only the smolvm system VM stages
# /exchange/systemd-run-request, which re-enters this job after a guest reboot.
# On the GitHub runner and the docker backend there is no marker path, so the
# job runs linearly and skips the reboot leg.
IA_PHASE_MARKER=""
if [ -n "${CI_EXCHANGE_DIR:-}" ] && [ -f "${CI_EXCHANGE_DIR}/systemd-run-request" ]; then
  IA_PHASE_MARKER="${CI_EXCHANGE_DIR}/ia-phase"
fi
if [ -n "${IA_PHASE_MARKER}" ] && [ -f "${IA_PHASE_MARKER}" ]; then
  ci_step "post-reboot persistence verification (phase B)"
  rm -f "${IA_PHASE_MARKER}"
  booted=1
  for _ in $(seq 1 90); do
    if systemctl is-active --quiet veil.service && ${SUDO} test -S /run/veil/helper.sock; then
      booted=0
      break
    fi
    sleep 1
  done
  [ "${booted}" -eq 0 ] || ci_die "veil.service did not come back after guest reboot"
  systemctl is-active --quiet veil-helper.socket || ci_die "veil-helper.socket inactive after reboot"
  assert_protocol_units "after reboot"
  ${SUDO} test -f /etc/veil/generated/hysteria2/ci-hy2.yaml || ci_die "hysteria2 generated config lost after reboot"
  ${SUDO} test -f /etc/veil/generated/mieru/server_config.json || ci_die "mieru generated config lost after reboot"
  ${SUDO} test -f /etc/veil/generated/olcrtc/ci-olc.yaml || ci_die "olcrtc generated config lost after reboot"
  ${SUDO} test -s /var/lib/veil/state.json || ci_die "state.json lost after reboot"
  ready=1
  for _ in $(seq 1 60); do
    if ${SUDO} /usr/local/bin/veil status --json >/dev/null 2>&1; then
      ready=0
      break
    fi
    sleep 1
  done
  [ "${ready}" -eq 0 ] || ci_die "panel did not become ready after reboot"
  ci_run veil-status-post-reboot ${SUDO} /usr/local/bin/veil status --json
  # Exit-0 alone is not readiness: the status payload must report the unit
  # active/running and the panel must answer an authenticated /healthz probe
  # — matching the same contract the first-install leg asserts.
  grep -q '"activeState": "active"' "${CI_ARTIFACT_DIR}/veil-status-post-reboot.log" \
    || ci_die "post-reboot status does not report an active service: $(cat "${CI_ARTIFACT_DIR}/veil-status-post-reboot.log")"
  grep -q '"subState": "running"' "${CI_ARTIFACT_DIR}/veil-status-post-reboot.log" \
    || ci_die "post-reboot status does not report a running service: $(cat "${CI_ARTIFACT_DIR}/veil-status-post-reboot.log")"
  # The install left live through the reboot is the custom-root leg (#746):
  # its env file carries the token/base path, and persistence of the custom
  # trees is itself part of the reboot contract.
  ${SUDO} test -s "${CUSTOM_VAR}/state.json" || ci_die "custom var state.json lost after reboot"
  ${SUDO} test -f "${CUSTOM_ETC}/veil.env" || ci_die "custom etc veil.env lost after reboot"
  ${SUDO} test -f "${CUSTOM_ETC}/generated/hysteria2/ci-hy2.yaml" \
    || ci_die "custom etc generated hysteria2 config lost after reboot"
  probe_panel_healthz "${CUSTOM_ETC}"
  # Custom-root teardown: the uninstall must remove the custom trees and stop
  # the units. The caddy leg below reinstalls default roots (#746).
  ci_run veil-uninstall-custom ${SUDO} /usr/local/bin/veil uninstall --yes --etc-dir "${CUSTOM_ETC}" --var-dir "${CUSTOM_VAR}"
  ${SUDO} test ! -e "${CUSTOM_ETC}" || ci_die "custom etc dir left after custom-root uninstall"
  ${SUDO} test ! -e "${CUSTOM_VAR}" || ci_die "custom var dir left after custom-root uninstall"
  if systemctl is-active --quiet veil.service; then
    ci_die "veil.service still active after custom-root uninstall"
  fi
  # The reboot leg previously skipped the entire caddy/ACME/public-route
  # contract — phase B exited right after the persistence checks. The CA
  # process does not survive reboot, so restart pebble here and run the same
  # caddy route leg before the shared uninstall leg (#405).
  start_pebble
  caddy_route_leg
  uninstall_leg
  ci_log "install-acceptance job passed (post-reboot)"
  exit 0
fi

ci_step "web/dist (embedded into the binary)"
bash "${CI_SCRIPTS_DIR}/prepare-frontend-dist.sh"

ci_step "build and install Veil"
go build -o /tmp/veil-install-acceptance ./cmd/veil
${SUDO} install -m 0755 /tmp/veil-install-acceptance /usr/local/bin/veil

# Pinned runtimes pre-seeded in the CI image keep the install deterministic;
# on a bare runner the product runtime phase downloads them itself.
ci_step "protocol runtimes (pinned when the image provides them)"
runtime_dir="${VEIL_CI_RUNTIME_DIR:-/opt/veil-runtime}"
for bin in hysteria mita caddy mieru naive olcrtc sing-box; do
  if [ -x "${runtime_dir}/${bin}" ]; then
    ${SUDO} install -m 0755 "${runtime_dir}/${bin}" "/usr/local/bin/${bin}"
  fi
done

# Pre-fetch any runtimes the image did not seed. GitHub release resolution
# flakes on shared runner egress IPs (rate limits, transient "no assets
# found"), and a missing runtime only surfaces minutes later inside the
# first-inbound leg — retry the fetch here so the install leg stays
# deterministic.
ci_step "protocol runtime download (retry on registry flakes)"
runtime_ok=1
for _ in $(seq 1 4); do
  if ${SUDO} /usr/local/bin/veil runtime install; then
    runtime_ok=0
    break
  fi
  sleep 10
done
[ "${runtime_ok}" -eq 0 ] || ci_die "runtime download failed after retries"
# A green `runtime install` is not evidence the binaries landed: skipped or
# partially-failed runtimes must not green the first-inbound leg (#409).
# This is the SERVER set the installer owns (hysteria2->hysteria,
# mieru->mita, naiveproxy->caddy, olcrtc->olcrtc, warp->sing-box); the
# mieru/naive CLIENT binaries are e2e tooling, not runtime-install output.
for bin in hysteria mita caddy olcrtc sing-box; do
  ${SUDO} test -x "/usr/local/bin/${bin}" \
    || ci_die "runtime install left ${bin} missing/non-executable in /usr/local/bin"
done
# Presence alone is not the pin contract: the installed sing-box must report
# the version verify_versions.py binds to CI_SINGBOX_TAG.
singbox_version="$(/usr/local/bin/sing-box version 2>/dev/null | grep -o '[0-9][0-9.]*' | head -1 || true)"
[ "v${singbox_version}" = "${CI_SINGBOX_TAG}" ] \
  || ci_die "sing-box reports version '${singbox_version:-none}', want ${CI_SINGBOX_TAG}"

# --- 1. Capability report: read-only, exit 0 on a supported host --------------
ci_step "install --check capability report (must not mutate)"
${SUDO} /usr/local/bin/veil install --check --panel-access direct --le-ip-cert=false --public-ip 127.0.0.1 --panel-port "${PANEL_PORT}" \
  | tee "${CI_ARTIFACT_DIR}/install-check.txt"
# Row-level asserts (issues #787/#818): each required check must appear with a
# real status — a bare "capability report"/"init" substring can be satisfied
# by a header line or unrelated prose while the actual checks never ran.
grep -q 'Install capability report' "${CI_ARTIFACT_DIR}/install-check.txt" \
  || ci_die "capability report header missing: $(cat "${CI_ARTIFACT_DIR}/install-check.txt")"
for row in platform init accounts sysctl; do
  grep -Eq "^${row}[[:space:]]+(ok|provision)[[:space:]]" "${CI_ARTIFACT_DIR}/install-check.txt" \
    || ci_die "capability report lacks a passing '${row}' row: $(cat "${CI_ARTIFACT_DIR}/install-check.txt")"
done
# The firewall row must exist too; its honest status depends on whether ufw
# was already present (ok|skipped) or needs provisioning (provision).
grep -Eq '^firewall[[:space:]]+(ok|provision|skipped)[[:space:]]' "${CI_ARTIFACT_DIR}/install-check.txt" \
  || ci_die "capability report lacks a 'firewall' row: $(cat "${CI_ARTIFACT_DIR}/install-check.txt")"
# A report that still lists a missing prerequisite must never exit 0.
if grep -Eq '^[a-z0-9-]+[[:space:]]+missing[[:space:]]' "${CI_ARTIFACT_DIR}/install-check.txt"; then
  ci_die "capability report lists missing prerequisites yet --check exited 0: $(cat "${CI_ARTIFACT_DIR}/install-check.txt")"
fi
if ${SUDO} test -e /var/lib/veil/state.json || ${SUDO} test -e /etc/veil/state.key; then
  ci_die "--check mutated the host (state files exist)"
fi

# --- 2. Real install ----------------------------------------------------------
ci_run veil-install ${SUDO} /usr/local/bin/veil install "${INSTALL_FLAGS[@]}"

# --- 3. Post-install contract -------------------------------------------------
ci_step "systemd unit contract"
systemctl is-active --quiet veil-helper.socket || ci_die "veil-helper.socket is not active"
systemctl is-active --quiet veil.service || ci_die "veil.service is not active"
systemctl is-enabled --quiet veil-helper.socket || ci_die "veil-helper.socket is not enabled"
systemctl is-enabled --quiet veil.service || ci_die "veil.service is not enabled"
${SUDO} test -S /run/veil/helper.sock || ci_die "helper socket /run/veil/helper.sock missing"

ci_step "state and configuration"
${SUDO} test -s /var/lib/veil/state.json || ci_die "state.json missing"
${SUDO} test -s /etc/veil/state.key || ci_die "state.key missing"
${SUDO} test -f /etc/veil/veil.env || ci_die "veil.env missing"
[ "$(${SUDO} stat -c '%a' /etc/veil/state.key)" = "640" ] || ci_die "state.key permissions are not 640"

ci_step "firewall contract (direct mode opens the panel port)"
if command -v ufw >/dev/null 2>&1; then
  ${SUDO} ufw status | tee "${CI_ARTIFACT_DIR}/ufw-status.txt"
  # Require an actual ALLOW rule — matching the port text alone greens a DENY
  # or a stale "Anywhere" comment column (issue #787). ufw renders v6 rules
  # as "2096/tcp (v6)  ALLOW  Anywhere (v6)".
  grep -Eq "^${PANEL_PORT}/tcp( \\(v6\\))?[[:space:]]+ALLOW[[:space:]]" "${CI_ARTIFACT_DIR}/ufw-status.txt" \
    || ci_die "ufw has no ALLOW rule for the panel port: $(cat "${CI_ARTIFACT_DIR}/ufw-status.txt")"
else
  ci_warn "ufw not present — install should have provisioned it; failing"
  exit 1
fi

ci_step "panel readiness through the product status probe"
ci_run veil-status ${SUDO} /usr/local/bin/veil status --json
grep -q '"activeState": "active"' "${CI_ARTIFACT_DIR}/veil-status.log" \
  || ci_die "veil status does not report an active service: $(cat "${CI_ARTIFACT_DIR}/veil-status.log")"
grep -q '"subState": "running"' "${CI_ARTIFACT_DIR}/veil-status.log" \
  || ci_die "veil status does not report a running service: $(cat "${CI_ARTIFACT_DIR}/veil-status.log")"
# The health contract lives under the secret web base path and needs the API token.
TOKEN="$(${SUDO} grep '^VEIL_API_TOKEN=' /etc/veil/veil.env | cut -d= -f2- | tr -d '[:space:]')"
BASE_PATH="$(${SUDO} grep '^VEIL_WEB_BASE_PATH=' /etc/veil/veil.env | cut -d= -f2- | tr -d '[:space:]')"
health_code="$(curl --http1.1 -sk --max-time 60 -o /tmp/ia-health.json -w '%{http_code}' -H "X-Veil-Token: ${TOKEN}" "https://127.0.0.1:${PANEL_PORT}${BASE_PATH}healthz")"
[ "${health_code}" = "200" ] || ci_die "panel /healthz returned HTTP ${health_code}"
# The payload must honestly report ok — an unhealthy panel still carries a
# "status" field, so matching the bare key would green a 200-lying response.
grep -q '"status":"ok"' /tmp/ia-health.json \
  || ci_die "panel /healthz did not report status ok: $(cat /tmp/ia-health.json)"

# --- 4. First-inbound acceptance per protocol (audit #313) ---------------------
# Create the first inbound of every protocol feasible on a direct install
# through the real management API, apply it, then verify the rendered config,
# the systemd unit, the apply revision, and the export/client-link shape.
# naiveproxy is intentionally absent from this leg: it rides the panel Caddy
# forward proxy, so it cannot run on a direct-mode install. The caddy-mode leg
# at the end of this script owns only the caddy install, real ACME issuance,
# and the public :443 panel route — it does NOT create a naiveproxy inbound.
# NaiveProxy protocol/data-path coverage is owned by scripts/ci/e2e.sh
# (TestRequiredNaiveProxyDataPath). A narrow naive create+apply cohabitation
# check on the caddy leg is a possible future extension, not coverage this
# job claims today (issue #351).
ci_step "first-inbound acceptance via the management API"
TOKEN="$(${SUDO} grep '^VEIL_API_TOKEN=' /etc/veil/veil.env | cut -d= -f2- | tr -d '[:space:]')"
[ -n "${TOKEN}" ] || ci_die "VEIL_API_TOKEN missing from /etc/veil/veil.env"
# VEIL_WEB_BASE_PATH already carries leading and trailing slashes.
API="https://127.0.0.1:${PANEL_PORT}${BASE_PATH%/}"

api_code() { # method path [body] -> http code; response body in /tmp/ia-resp.json
  local method="$1" path="$2" body="${3:-}" timeout=60
  # Mutations are serialized behind live validation plus a synchronous
  # auto-apply; give POSTs a generous ceiling.
  [ "${method}" = "POST" ] && timeout=300
  if [ -n "${body}" ]; then
    curl --http1.1 -sk --max-time "${timeout}" -o /tmp/ia-resp.json -w '%{http_code}' -X "${method}" \
      -H "X-Veil-Token: ${TOKEN}" -H 'Content-Type: application/json' \
      -d "${body}" "${API}${path}"
  else
    curl --http1.1 -sk --max-time "${timeout}" -o /tmp/ia-resp.json -w '%{http_code}' -X "${method}" \
      -H "X-Veil-Token: ${TOKEN}" "${API}${path}"
  fi
}

# The panel's startup reconcile can hold the mutation lock right after
# install; wait for a management read to succeed before posting mutations.
api_ready=1
for _ in $(seq 1 24); do
  ready_code="$(api_code GET /api/apply/state || true)"
  if [ "${ready_code}" = "200" ]; then
    api_ready=0
    break
  fi
  sleep 5
done
[ "${api_ready}" -eq 0 ] || ci_die "management API did not become ready (last code ${ready_code:-none})"

create_inbound() { # label body
  local label="$1" code
  code="$(api_code POST /api/inbounds "$2")"
  [ "${code}" = "201" ] || ci_die "create inbound ${label}: HTTP ${code}: $(cat /tmp/ia-resp.json)"
}

apply_panel() {
  local code
  code="$(api_code POST /api/apply/plan)"
  [ "${code}" = "200" ] || ci_die "apply plan: HTTP ${code}: $(cat /tmp/ia-resp.json)"
  # Drive the full live+services apply through the explicit endpoint — the
  # same path the panel's apply button takes — instead of stage-only confirm,
  # and require the response to honestly report the apply happened.
  code="$(api_code POST /api/apply '{"confirm":true,"applyLive":true,"applyServices":true}')"
  [ "${code}" = "200" ] || ci_die "apply confirm: HTTP ${code}: $(cat /tmp/ia-resp.json)"
  grep -q '"applied":true' /tmp/ia-resp.json \
    || ci_die "apply confirm did not report applied=true: $(cat /tmp/ia-resp.json)"
}

create_inbound ci-hy2 '{"name":"ci-hy2","protocol":"hysteria2","transport":"udp","port":34443,"enabled":true,"password":"ci-pass"}'
create_inbound ci-mieru-tcp '{"name":"ci-mieru-tcp","protocol":"mieru","transport":"tcp","port":34444,"enabled":true,"profiles":[{"name":"alice","password":"alice-pass","enabled":true}]}'
create_inbound ci-mieru-udp '{"name":"ci-mieru-udp","protocol":"mieru","transport":"udp","port":34445,"enabled":true,"password":"udp-pass"}'
create_inbound ci-olc '{"name":"ci-olc","protocol":"olcrtc","transport":"udp","port":34446,"enabled":true,"protocolFields":{"password":"abababababababababababababababababababababababababababababababab","olcrtcAuth":"jitsi","olcrtcTransport":"datachannel","olcrtcRoomID":"https://127.0.0.1:1/veil-ci"}}'

# Durable idempotency contract: replaying the same mutation with the same
# Idempotency-Key must return the original response, not a duplicate-name 409
# and not a divergent 201 — the stored response is replayed byte-for-byte.
idem_code() { # body [headers-file]
  local extra=()
  [ -n "${2:-}" ] && extra=(-D "$2")
  curl --http1.1 -sk --max-time 300 -o /tmp/ia-resp.json -w '%{http_code}' -X POST \
    -H "X-Veil-Token: ${TOKEN}" -H 'Content-Type: application/json' \
    -H "Idempotency-Key: ci-acceptance-mieru-idem" "${extra[@]}" -d "$1" "${API}/api/inbounds"
}
idem_body='{"name":"ci-mieru-idem","protocol":"mieru","transport":"tcp","port":34447,"enabled":true,"password":"idem-pass"}'
first="$(idem_code "${idem_body}")"
cp /tmp/ia-resp.json /tmp/ia-idem-first.json
second="$(idem_code "${idem_body}" /tmp/ia-idem-second.headers)"
cp /tmp/ia-resp.json /tmp/ia-idem-second.json
[ "${first}" = "201" ] || ci_die "idempotent create: first HTTP ${first}: $(cat /tmp/ia-idem-first.json)"
[ "${second}" = "201" ] || ci_die "idempotent replay did not return the original 201 (got ${second}): $(cat /tmp/ia-idem-second.json)"
cmp -s /tmp/ia-idem-first.json /tmp/ia-idem-second.json \
  || ci_die "idempotent replay body diverged: first=$(cat /tmp/ia-idem-first.json) second=$(cat /tmp/ia-idem-second.json)"
grep -qi 'idempotency-replayed: true' /tmp/ia-idem-second.headers \
  || ci_die "idempotent replay did not set the Idempotency-Replayed header: $(cat /tmp/ia-idem-second.headers)"

apply_panel
api_code GET /api/apply/state
cp /tmp/ia-resp.json "${CI_ARTIFACT_DIR}/apply-state-after-inbounds.json"
grep -q '"state":"synced"' "${CI_ARTIFACT_DIR}/apply-state-after-inbounds.json" \
  || ci_die "apply state did not reach synced: $(cat "${CI_ARTIFACT_DIR}/apply-state-after-inbounds.json")"
# synced is derived as desired <= applied — prove the applied revision really
# caught up to the desired revision the mutations produced, not merely that
# the state string looks healthy.
desired_rev="$(grep -o -m1 '"desiredRevision":[0-9]*' "${CI_ARTIFACT_DIR}/apply-state-after-inbounds.json" | cut -d: -f2 || true)"
applied_rev="$(grep -o -m1 '"appliedRevision":[0-9]*' "${CI_ARTIFACT_DIR}/apply-state-after-inbounds.json" | cut -d: -f2 || true)"
{ [ -n "${desired_rev}" ] && [ "${desired_rev}" = "${applied_rev}" ]; } \
  || ci_die "applied revision did not catch up to desired (applied=${applied_rev:-none} desired=${desired_rev:-none})"

ci_step "firewall inbound contract (ufw carries the applied inbound ports)"
# The apply's firewall transaction must leave ufw allow rules for every
# applied inbound port — the panel-port check above cannot see a missing
# inbound hole. olcRTC has no runtime listener (its FirewallService is empty:
# the panel side terminates the datachannel), so port 34446 expects no rule.
${SUDO} ufw status | tee "${CI_ARTIFACT_DIR}/ufw-status-after-inbounds.txt"
for port_spec in 34443/udp 34444/tcp 34445/udp 34447/tcp; do
  grep -Eq "^${port_spec}( \\(v6\\))?[[:space:]]+ALLOW[[:space:]]" "${CI_ARTIFACT_DIR}/ufw-status-after-inbounds.txt" \
    || ci_die "ufw has no ALLOW rule for inbound ${port_spec}: $(cat "${CI_ARTIFACT_DIR}/ufw-status-after-inbounds.txt")"
done

ci_step "rendered configs"
${SUDO} test -f /etc/veil/generated/hysteria2/ci-hy2.yaml || ci_die "hysteria2 config not rendered"
${SUDO} test -f /etc/veil/generated/mieru/server_config.json || ci_die "mieru config not rendered"
${SUDO} test -f /etc/veil/generated/olcrtc/ci-olc.yaml || ci_die "olcrtc config not rendered"

ci_step "protocol service lifecycle"
# olcRTC needs an external conferencing provider at runtime; is-enabled alone
# can green a unit that never reached ExecStart, so the first leg shares the
# same start-once contract as assert_protocol_units (#408 residual, #669) —
# which also covers hy2/mieru is-active.
assert_protocol_units "first lifecycle leg"
# veil-helper.service is a static socket-activated unit (no [Install]) — the
# correct contract is "running once the socket was used", and the apply legs
# above provably drove privileged ops (service actions, ufw) through it.
systemctl is-active --quiet veil-helper.service \
  || ci_die "veil-helper.service not active after privileged apply traffic"

ci_step "export/client-link shape"
api_code GET /api/client-links
cp /tmp/ia-resp.json "${CI_ARTIFACT_DIR}/client-links.json"
for proto in hysteria2 mieru olcrtc; do
  grep -q "\"${proto}\"" "${CI_ARTIFACT_DIR}/client-links.json" \
    || ci_die "client-links missing protocol ${proto}: $(cat "${CI_ARTIFACT_DIR}/client-links.json")"
done

# --- Traffic telemetry contract (all protocols) --------------------------------
# The collector polls real runtime accounting; hysteria2 is the only
# accounting-capable protocol in this matrix. Assert that (a) a provider is
# registered for ci-hy2 and reaches healthy — proving the management plane
# authenticates with the credential the running unit actually serves
# (regression: deriving the secret from the applied snapshot left telemetry
# at a permanent HTTP 401 after a partially failed apply), and (b) clients on
# non-accounting protocols honestly report unsupported instead of fake zeros.
ci_step "traffic telemetry contract (all protocols)"

# Clients bound to each first inbound exercise the whole pipeline: binding
# identities land in the rendered configs and in the provider's attribution
# map through the same auto-apply a panel mutation would run.
create_client() { # label body -> response in /tmp/ia-resp.json
  local label="$1" code
  code="$(api_code POST /api/v1/clients "$2")"
  [ "${code}" = "201" ] || ci_die "create client ${label}: HTTP ${code}: $(cat /tmp/ia-resp.json)"
}
create_client ci-traffic-hy2 '{"name":"ci-traffic-hy2","bindings":[{"inboundId":"ci-hy2"}]}'
hy2_client="$(grep -o '"client":{"id":"[^"]*"' /tmp/ia-resp.json | head -1 | cut -d'"' -f6)"
[ -n "${hy2_client}" ] || ci_die "hy2 traffic client id missing: $(cat /tmp/ia-resp.json)"
create_client ci-traffic-mieru '{"name":"ci-traffic-mieru","bindings":[{"inboundId":"ci-mieru-tcp"}]}'
mieru_client="$(grep -o '"client":{"id":"[^"]*"' /tmp/ia-resp.json | head -1 | cut -d'"' -f6)"
[ -n "${mieru_client}" ] || ci_die "mieru traffic client id missing: $(cat /tmp/ia-resp.json)"
create_client ci-traffic-olc '{"name":"ci-traffic-olc","bindings":[{"inboundId":"ci-olc"}]}'
olc_client="$(grep -o '"client":{"id":"[^"]*"' /tmp/ia-resp.json | head -1 | cut -d'"' -f6)"
[ -n "${olc_client}" ] || ci_die "olc traffic client id missing: $(cat /tmp/ia-resp.json)"

# The collector polls every 30s; give the first observation plus the provider
# rebuild after the client-mutation applies room to land. A provider's first
# observation can race the unit's traffic listener coming back after the apply
# restart — a transient degraded must not end the poll; the post-loop checks
# still fail the leg on a persistent degraded or a 401.
telemetry=1
summary=""
for _ in $(seq 1 45); do
  code="$(api_code GET /api/v1/traffic/summary || true)"
  if [ "${code}" = "200" ]; then
    summary="$(cat /tmp/ia-resp.json)"
    # The TOP-LEVEL summary state is authoritative: Go marshals the response
    # map with "state" after the providers array, so it is the last "state"
    # field. A substring match would let a nested provider's healthy state
    # end the wait while another provider is still degraded.
    summary_state="$(grep -o '"state":"[^"]*"' /tmp/ia-resp.json | tail -1 || true)"
    if [ "${summary_state}" = '"state":"healthy"' ]; then
      telemetry=0
      break
    fi
  fi
  sleep 2
done
printf '%s\n' "${summary}" > "${CI_ARTIFACT_DIR}/traffic-summary.json"
[ "${telemetry}" -eq 0 ] || ci_die "traffic summary did not reach healthy: ${summary}"
grep -q '"providerCount":2' "${CI_ARTIFACT_DIR}/traffic-summary.json" \
  || ci_die "expected hysteria2 and mieru providers to be registered: ${summary}"
grep -q '"key":"hysteria2:ci-hy2"' "${CI_ARTIFACT_DIR}/traffic-summary.json" \
  || ci_die "hysteria2:ci-hy2 provider not registered: ${summary}"
grep -q '"key":"mieru:server"' "${CI_ARTIFACT_DIR}/traffic-summary.json" \
  || ci_die "mieru:server provider not registered: ${summary}"
if grep -q '"state":"degraded"' "${CI_ARTIFACT_DIR}/traffic-summary.json"; then
  ci_die "a traffic provider is degraded: ${summary}"
fi
# Scope 401 to lastError values: a bare grep would match any unix timestamp
# ending in 401 (e.g. lastSuccessfulObservationAt) and false-positive the leg.
if grep -q '"lastError":"[^"]*401[^"]*"' "${CI_ARTIFACT_DIR}/traffic-summary.json"; then
  ci_die "a traffic provider hit an auth failure: ${summary}"
fi

# Per-client report state: the hysteria2 and mieru bindings support
# accounting (pending until first real traffic; healthy once observed), while
# olcRTC has no runtime accounting and must say so instead of fake health.
client_state() { # client-id -> the "state" value of the traffic report
  local code
  code="$(api_code GET "/api/v1/traffic/$1")"
  [ "${code}" = "200" ] || ci_die "traffic report for $1: HTTP ${code}: $(cat /tmp/ia-resp.json)"
  grep -o '"state":"[^"]*"' /tmp/ia-resp.json | head -1 | cut -d'"' -f4
}
hy2_state="$(client_state "${hy2_client}")"
case "${hy2_state}" in
  pending|healthy) ;;
  *) ci_die "hysteria2 client traffic state = '${hy2_state}', want pending|healthy" ;;
esac
mieru_state="$(client_state "${mieru_client}")"
case "${mieru_state}" in
  pending|healthy) ;;
  *) ci_die "mieru client traffic state = '${mieru_state}', want pending|healthy" ;;
esac
olc_state="$(client_state "${olc_client}")"
[ "${olc_state}" = "unsupported" ] \
  || ci_die "olcRTC client traffic state = '${olc_state}', want unsupported"

# --- 5. Reinstall idempotency -------------------------------------------------
ci_step "idempotent reinstall (existing state reused)"
ci_run veil-reinstall ${SUDO} /usr/local/bin/veil install "${INSTALL_FLAGS[@]}"
grep -q "Reused the existing admin login" "${CI_ARTIFACT_DIR}/veil-reinstall.log" \
  || ci_die "reinstall did not reuse the existing state"
systemctl is-active --quiet veil.service || ci_die "veil.service not active after reinstall"
# The reused-state install must leave the first-inbound units standing — a
# reinstall that kills protocol units would still green the veil.service check.
assert_protocol_units "after reinstall"
ci_run veil-status-after-reinstall ${SUDO} /usr/local/bin/veil status --json

# --- 6. Controlled ACME CA issuance (audit #304) -------------------------------
# Issue a REAL certificate against a pinned pebble test CA instead of public
# Let's Encrypt. VEIL_ACME_CA_URL switches the acme.sh --server value and
# VEIL_ACME_INSECURE skips TLS verification of pebble's self-signed endpoint.
ci_step "controlled ACME CA (pebble) real issuance"
# start_pebble always installs the pinned version (#454) and is shared with
# the post-reboot phase (#405). PEBBLE_MOD / PEBBLE_ISSUING_ROOT are set for
# the caddy leg below.
start_pebble
# Pebble's VA validates HTTP-01 against httpPort 5002 (test/config).
ci_run veil-install-pebble ${SUDO} env \
  VEIL_ACME_CA_URL=https://127.0.0.1:14000/dir VEIL_ACME_INSECURE=1 \
  /usr/local/bin/veil install --yes --panel-access direct \
  --le-ip-cert --le-ip-cert-port 5002 --public-ip 127.0.0.1 --panel-port "${PANEL_PORT}"
${SUDO} openssl x509 -in /etc/veil/panel/tls.crt -noout -issuer | tee "${CI_ARTIFACT_DIR}/panel-cert-issuer.txt"
grep -qi "pebble" "${CI_ARTIFACT_DIR}/panel-cert-issuer.txt" \
  || ci_die "panel certificate was not issued by the controlled CA: $(cat "${CI_ARTIFACT_DIR}/panel-cert-issuer.txt")"
${SUDO} systemctl restart veil.service
sleep 2
openssl s_client -connect "127.0.0.1:${PANEL_PORT}" -servername 127.0.0.1 </dev/null 2>/dev/null \
  | openssl x509 -noout -issuer | tee "${CI_ARTIFACT_DIR}/panel-served-issuer.txt"
grep -qi "pebble" "${CI_ARTIFACT_DIR}/panel-served-issuer.txt" \
  || ci_die "panel is not serving the pebble-issued certificate"
systemctl is-active --quiet veil.service || ci_die "veil.service not active after controlled-CA reinstall"
# The ACME reinstall restarted the panel; the protocol units from the
# first-inbound leg must still be standing as well.
assert_protocol_units "after controlled-CA reinstall"

# --- 6.5. Custom --etc-dir/--var-dir install (issue #746) --------------------
# The legs above only exercise the packaged /etc/veil + /var/lib/veil layout.
# Install again under custom roots and leave it LIVE through the smolvm reboot
# so phase B proves the custom trees, rendered units, and panel persist. On
# the linear path the custom install is torn down right here and the caddy
# leg below reinstalls default roots.
ci_step "custom-root install (--etc-dir/--var-dir)"
${SUDO} rm -rf "${CUSTOM_ROOT}"
ci_run veil-install-custom ${SUDO} /usr/local/bin/veil install "${INSTALL_FLAGS[@]}" --etc-dir "${CUSTOM_ETC}" --var-dir "${CUSTOM_VAR}"
systemctl is-active --quiet veil.service || ci_die "custom-root veil.service not active"
systemctl is-enabled --quiet veil.service || ci_die "custom-root veil.service not enabled"
systemctl is-active --quiet veil-helper.socket || ci_die "custom-root helper socket not active"
${SUDO} test -S /run/veil/helper.sock || ci_die "helper socket missing under custom roots"
${SUDO} test -s "${CUSTOM_ETC}/state.key" || ci_die "custom state.key missing"
${SUDO} test -f "${CUSTOM_ETC}/veil.env" || ci_die "custom veil.env missing"
${SUDO} test -s "${CUSTOM_VAR}/state.json" || ci_die "custom state.json missing"
# The rendered units must carry the custom roots — merged `systemctl cat`
# output covers both the generated-unit and drop-in layouts, so neither
# placement can green this while pointing at /etc/veil or /var/lib/veil.
${SUDO} systemctl cat veil.service > "${CI_ARTIFACT_DIR}/veil-custom-unit.txt"
grep -q "EnvironmentFile=-${CUSTOM_ETC}/veil.env" "${CI_ARTIFACT_DIR}/veil-custom-unit.txt" \
  || ci_die "veil.service does not reference the custom etc dir"
grep -q "VEIL_STATE_PATH=${CUSTOM_VAR}/state.json" "${CI_ARTIFACT_DIR}/veil-custom-unit.txt" \
  || ci_die "veil.service does not reference the custom var dir"
grep -q "ReadWritePaths=${CUSTOM_VAR}" "${CI_ARTIFACT_DIR}/veil-custom-unit.txt" \
  || ci_die "veil.service does not grant the custom var dir"
${SUDO} systemctl cat veil-helper.service > "${CI_ARTIFACT_DIR}/veil-helper-custom-unit.txt"
grep -q "VEIL_KEY_PATH=${CUSTOM_ETC}/state.key" "${CI_ARTIFACT_DIR}/veil-helper-custom-unit.txt" \
  || ci_die "veil-helper.service does not reference the custom etc dir"
grep -q "ReadWritePaths=${CUSTOM_ETC} ${CUSTOM_VAR}" "${CI_ARTIFACT_DIR}/veil-helper-custom-unit.txt" \
  || ci_die "veil-helper.service does not grant the custom roots"
# The edge units must mask the CUSTOM var dir, not the packaged default.
${SUDO} systemctl cat veil-hysteria2@.service > "${CI_ARTIFACT_DIR}/veil-hy2-custom-unit.txt"
grep -q "InaccessiblePaths=/run/veil/helper.sock ${CUSTOM_VAR}" "${CI_ARTIFACT_DIR}/veil-hy2-custom-unit.txt" \
  || ci_die "veil-hysteria2@ does not mask the custom var dir"
probe_panel_healthz "${CUSTOM_ETC}"
# Re-apply the protocol inbounds under the custom roots so the reboot leg
# proves the units persist with their configs under the custom etc tree —
# an install-only leg would leave the templates without rendered configs.
TOKEN="$(${SUDO} grep '^VEIL_API_TOKEN=' "${CUSTOM_ETC}/veil.env" | cut -d= -f2- | tr -d '[:space:]')"
BASE_PATH="$(${SUDO} grep '^VEIL_WEB_BASE_PATH=' "${CUSTOM_ETC}/veil.env" | cut -d= -f2- | tr -d '[:space:]')"
API="https://127.0.0.1:${PANEL_PORT}${BASE_PATH%/}"
api_ready=1
for _ in $(seq 1 24); do
  ready_code="$(api_code GET /api/apply/state || true)"
  if [ "${ready_code}" = "200" ]; then
    api_ready=0
    break
  fi
  sleep 5
done
[ "${api_ready}" -eq 0 ] || ci_die "custom-root management API did not become ready (last code ${ready_code:-none})"
create_inbound ci-hy2 '{"name":"ci-hy2","protocol":"hysteria2","transport":"udp","port":34443,"enabled":true,"password":"ci-pass"}'
create_inbound ci-mieru-tcp '{"name":"ci-mieru-tcp","protocol":"mieru","transport":"tcp","port":34444,"enabled":true,"profiles":[{"name":"alice","password":"alice-pass","enabled":true}]}'
create_inbound ci-mieru-udp '{"name":"ci-mieru-udp","protocol":"mieru","transport":"udp","port":34445,"enabled":true,"password":"udp-pass"}'
create_inbound ci-olc '{"name":"ci-olc","protocol":"olcrtc","transport":"udp","port":34446,"enabled":true,"protocolFields":{"password":"abababababababababababababababababababababababababababababababab","olcrtcAuth":"jitsi","olcrtcTransport":"datachannel","olcrtcRoomID":"https://127.0.0.1/veil-ci"}}'
apply_panel
assert_protocol_units "custom-root leg"
if [ -z "${IA_PHASE_MARKER}" ]; then
  # Linear path: tear the custom install down now; on the smolvm path phase B
  # verifies reboot persistence first and tears down there.
  ci_step "custom-root uninstall"
  ci_run veil-uninstall-custom ${SUDO} /usr/local/bin/veil uninstall --yes --etc-dir "${CUSTOM_ETC}" --var-dir "${CUSTOM_VAR}"
  ${SUDO} test ! -e "${CUSTOM_ETC}" || ci_die "custom etc dir left after custom-root uninstall"
  ${SUDO} test ! -e "${CUSTOM_VAR}" || ci_die "custom var dir left after custom-root uninstall"
  if systemctl is-active --quiet veil.service; then
    ci_die "veil.service still active after custom-root uninstall"
  fi
fi

# --- 7. Reboot persistence (smolvm system VM only) ---------------------------
if [ -n "${IA_PHASE_MARKER}" ]; then
  ci_log "smolvm system VM: staging post-reboot phase and rebooting"
  echo phase-b > "${IA_PHASE_MARKER}"
  sync
  systemctl reboot
  sleep 30
  ci_die "guest reboot did not take effect"
fi

# --- 8. Caddy panel proxy leg (audit #304: verify the public route) ----------
# The legs above install with --panel-access direct; this leg proves the caddy
# reverse-proxy path on the same host. It runs identically in the post-reboot
# phase (smolvm) so the reboot leg no longer skips the public-route contract
# (#405); see caddy_route_leg for the probe semantics.
caddy_route_leg

uninstall_leg

# --- 9. Native package lifecycle on live systemd (issue #503) -----------------
# The legs above install through the binary/curl-style path, which never
# exercises the packaged maintainer scripts, the /lib vendor units, or the
# sysctl drop-in. Install the real .deb here, prove the packaged contract under
# REAL systemd, purge, and finish through the leftover-path uninstaller — the
# same cleanup an operator gets after `apt remove veil`.
ci_step "native package lifecycle (nfpm .deb on live systemd)"
nfpm_bin="$(go env GOPATH)/bin/nfpm"
if [ ! -x "${nfpm_bin}" ]; then
  GOBIN="$(go env GOPATH)/bin" go install "github.com/goreleaser/nfpm/v2/cmd/nfpm@${CI_NFPM_VERSION}"
fi
# The acceptance binary was built earlier; nfpm packages dist/veil.
mkdir -p dist /tmp/veil-pkg
cp /tmp/veil-install-acceptance dist/veil
# The .deb arch must match the runner (the arm64 leg cannot install an amd64
# package — issue #503 exercises this job on both architectures).
pkg_arch="$(dpkg --print-architecture)"
export VEIL_VERSION="9.9.9" VEIL_ARCH="${pkg_arch}" VEIL_MAINTAINER="Veil CI <veil@users.noreply.github.com>"
"${nfpm_bin}" package --config packaging/nfpm.yaml --packager deb --target /tmp/veil-pkg

${SUDO} apt-get install -y /tmp/veil-pkg/*.deb
test -f /lib/systemd/system/veil.service || ci_die "packaged veil.service missing from /lib/systemd/system"
test -f /lib/systemd/system/veil-helper.socket || ci_die "packaged veil-helper.socket missing"
test -f /etc/sysctl.d/99-veil-quic.conf || ci_die "QUIC sysctl drop-in missing after package install"
# postinstall ran under REAL systemd: the helper socket enable must have
# produced an actual wants symlink, and the packaged accounts exist.
systemctl is-enabled --quiet veil-helper.socket \
  || ci_die "postinstall did not enable veil-helper.socket on live systemd"
id veil >/dev/null || ci_die "veil user missing after package install"
id veil-proxy >/dev/null || ci_die "veil-proxy user missing after package install"
id veil-mita >/dev/null || ci_die "veil-mita user missing after package install"
id -nG veil | tr ' ' '\n' | grep -qx veil-proxy || ci_die "veil not in veil-proxy group"
id -nG veil | tr ' ' '\n' | grep -qx veil-mita || ci_die "veil not in veil-mita group"

# Legacy pre-consolidation per-inbound Caddy leftovers (issue #375): a unit
# file plus its enablement wants link must not survive package removal —
# preremove stops/disables the instance and sweeps the want, postremove clears
# the vendor-dir unit file.
${SUDO} install -m 0644 /dev/null /lib/systemd/system/veil-caddy@legacy.service
${SUDO} mkdir -p /etc/systemd/system/multi-user.target.wants
${SUDO} ln -sf /lib/systemd/system/veil-caddy@legacy.service \
  /etc/systemd/system/multi-user.target.wants/veil-caddy@legacy.service

${SUDO} apt-get purge -y veil
# Remove+purge must leave no packaged payload — the units and sysctl drop-in
# are package-owned, not conffiles (issues #475, #485).
for unit in veil.service veil-helper.service veil-helper.socket veil-backup.service veil-backup.timer veil-caddy.service veil-hysteria2@.service veil-mieru.service veil-olcrtc@.service veil-warp.service; do
  test ! -e "/lib/systemd/system/${unit}" || ci_die "packaged unit ${unit} left after purge"
done
test ! -e /etc/sysctl.d/99-veil-quic.conf || ci_die "sysctl drop-in left after purge"
test ! -e /usr/local/bin/veil || ci_die "veil binary left after purge"
test ! -e /lib/systemd/system/veil-caddy@legacy.service \
  || ci_die "legacy veil-caddy@ unit file left in vendor dir after purge"
test ! -e /etc/systemd/system/multi-user.target.wants/veil-caddy@legacy.service \
  || ci_die "legacy veil-caddy@ wants link left after purge"
if systemctl is-enabled --quiet veil-helper.socket 2>/dev/null; then
  ci_die "veil-helper.socket still enabled after purge"
fi

# The package purge preserves /etc/veil and /var/lib/veil (operator state).
# Finish through the standalone leftover-path uninstaller — it must detect and
# clear them, and report a clean host on a second pass (issues #500, #501).
ci_step "leftover-state uninstaller after package purge"
# Plant every wants-link/drop-in shape the uninstaller must clear (issue
# #788): non-template service links, the helper socket link, the backup timer
# link, and per-unit drop-in dirs. Links dangle post-purge (their vendor
# targets are gone) — a leftover pass that only saw /etc/veil would never
# prove these globs.
${SUDO} mkdir -p /etc/systemd/system/multi-user.target.wants \
  /etc/systemd/system/sockets.target.wants \
  /etc/systemd/system/timers.target.wants \
  /etc/systemd/system/veil.service.d \
  /etc/systemd/system/veil-helper.socket.d \
  /etc/systemd/system/veil-backup.timer.d
${SUDO} ln -sf /nonexistent/veil.service /etc/systemd/system/multi-user.target.wants/veil.service
${SUDO} ln -sf /nonexistent/veil-helper.socket /etc/systemd/system/sockets.target.wants/veil-helper.socket
${SUDO} ln -sf /nonexistent/veil-backup.timer /etc/systemd/system/timers.target.wants/veil-backup.timer
${SUDO} ln -sf /nonexistent/veil-olcrtc@ci-olc.service /etc/systemd/system/multi-user.target.wants/veil-olcrtc@ci-olc.service
for dropin in veil.service.d veil-helper.socket.d veil-backup.timer.d; do
  printf '[Service]\n' | ${SUDO} tee "/etc/systemd/system/${dropin}/10-veil-install.conf" >/dev/null
done
ci_run veil-uninstall-leftover ${SUDO} bash scripts/uninstall.sh --yes
${SUDO} test ! -e /etc/veil || ci_die "/etc/veil left after leftover uninstall"
${SUDO} test ! -e /var/lib/veil || ci_die "/var/lib/veil left after leftover uninstall"
for leftover in \
  /etc/systemd/system/multi-user.target.wants/veil.service \
  /etc/systemd/system/multi-user.target.wants/veil-olcrtc@ci-olc.service \
  /etc/systemd/system/sockets.target.wants/veil-helper.socket \
  /etc/systemd/system/timers.target.wants/veil-backup.timer \
  /etc/systemd/system/veil.service.d \
  /etc/systemd/system/veil-helper.socket.d \
  /etc/systemd/system/veil-backup.timer.d; do
  if ${SUDO} test -e "${leftover}" || ${SUDO} test -L "${leftover}"; then
    ci_die "planted leftover ${leftover} survived the uninstaller"
  fi
done
bash scripts/uninstall.sh --yes 2>&1 | tee "${CI_ARTIFACT_DIR}/uninstall-second-pass.log"
grep -q "Nothing to uninstall" "${CI_ARTIFACT_DIR}/uninstall-second-pass.log" \
  || ci_die "second uninstall pass still finds leftover state"

ci_log "install-acceptance job passed"
