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

PANEL_PORT=2096
INSTALL_FLAGS=(--yes --panel-access direct --le-ip-cert=false --public-ip 127.0.0.1 --panel-port "${PANEL_PORT}")

uninstall_leg() {
  ci_step "uninstall --keep-data"
  ci_run veil-uninstall ${SUDO} /usr/local/bin/veil uninstall --yes --keep-data
  if systemctl is-active --quiet veil.service; then
    ci_die "veil.service still active after uninstall"
  fi
  ${SUDO} test -s /var/lib/veil/state.json || ci_die "--keep-data removed state.json"
  ${SUDO} journalctl --no-pager -u veil.service -u veil-helper.service -u veil-helper.socket \
    > "${CI_ARTIFACT_DIR}/veil-units-journal.txt" 2>&1 || true
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
  systemctl is-active --quiet veil-hysteria2@ci-hy2.service || ci_die "veil-hysteria2@ci-hy2 inactive after reboot"
  systemctl is-active --quiet veil-mieru.service || ci_die "veil-mieru inactive after reboot"
  ${SUDO} test -f /etc/veil/generated/hysteria2/ci-hy2.yaml || ci_die "generated config lost after reboot"
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

# --- 1. Capability report: read-only, exit 0 on a supported host --------------
ci_step "install --check capability report (must not mutate)"
${SUDO} /usr/local/bin/veil install --check --panel-access direct --le-ip-cert=false --public-ip 127.0.0.1 --panel-port "${PANEL_PORT}" \
  | tee "${CI_ARTIFACT_DIR}/install-check.txt"
grep -q "capability report" "${CI_ARTIFACT_DIR}/install-check.txt"
grep -q "init" "${CI_ARTIFACT_DIR}/install-check.txt"
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
  grep -q "${PANEL_PORT}/tcp" "${CI_ARTIFACT_DIR}/ufw-status.txt" || ci_die "ufw has no allow rule for the panel port"
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
grep -q '"status"' /tmp/ia-health.json || ci_die "panel /healthz payload unexpected: $(cat /tmp/ia-health.json)"

# --- 4. First-inbound acceptance per protocol (audit #313) ---------------------
# Create the first inbound of every protocol feasible on a direct install
# through the real management API, apply it, then verify the rendered config,
# the systemd unit, the apply revision, and the export/client-link shape.
# naiveproxy is intentionally absent here: it rides the panel Caddy forward
# proxy and belongs to the caddy-mode install matrix leg (controlled ACME CA).
ci_step "first-inbound acceptance via the management API"
TOKEN="$(${SUDO} grep '^VEIL_API_TOKEN=' /etc/veil/veil.env | cut -d= -f2- | tr -d '[:space:]')"
[ -n "${TOKEN}" ] || ci_die "VEIL_API_TOKEN missing from /etc/veil/veil.env"
# VEIL_WEB_BASE_PATH already carries leading and trailing slashes.
API="https://127.0.0.1:${PANEL_PORT}${BASE_PATH%/}"

api_code() { # method path [body] -> http code; response body in /tmp/ia-resp.json
  local method="$1" path="$2" body="${3:-}"
  if [ -n "${body}" ]; then
    curl --http1.1 -sk --max-time 60 -o /tmp/ia-resp.json -w '%{http_code}' -X "${method}" \
      -H "X-Veil-Token: ${TOKEN}" -H 'Content-Type: application/json' \
      -d "${body}" "${API}${path}"
  else
    curl --http1.1 -sk --max-time 60 -o /tmp/ia-resp.json -w '%{http_code}' -X "${method}" \
      -H "X-Veil-Token: ${TOKEN}" "${API}${path}"
  fi
}

create_inbound() { # label body
  local label="$1" code
  code="$(api_code POST /api/inbounds "$2")"
  [ "${code}" = "201" ] || ci_die "create inbound ${label}: HTTP ${code}: $(cat /tmp/ia-resp.json)"
}

apply_panel() {
  local code
  code="$(api_code POST /api/apply/plan)"
  [ "${code}" = "200" ] || ci_die "apply plan: HTTP ${code}: $(cat /tmp/ia-resp.json)"
  code="$(api_code POST /api/apply '{"confirm":true}')"
  [ "${code}" = "200" ] || ci_die "apply confirm: HTTP ${code}: $(cat /tmp/ia-resp.json)"
}

create_inbound ci-hy2 '{"name":"ci-hy2","protocol":"hysteria2","transport":"udp","port":34443,"enabled":true,"password":"ci-pass"}'
create_inbound ci-mieru-tcp '{"name":"ci-mieru-tcp","protocol":"mieru","transport":"tcp","port":34444,"enabled":true,"profiles":[{"name":"alice","password":"alice-pass","enabled":true}]}'
create_inbound ci-mieru-udp '{"name":"ci-mieru-udp","protocol":"mieru","transport":"udp","port":34445,"enabled":true,"password":"udp-pass"}'
create_inbound ci-olc '{"name":"ci-olc","protocol":"olcrtc","transport":"udp","port":34446,"enabled":true,"protocolFields":{"password":"abababababababababababababababababababababababababababababababab","olcrtcAuth":"jitsi","olcrtcTransport":"datachannel","olcrtcRoomID":"https://127.0.0.1:1/veil-ci"}}'

# Durable idempotency contract: replaying the same mutation with the same
# Idempotency-Key must return the original response, not a duplicate-name 409.
idem_code() { # body
  curl --http1.1 -sk --max-time 60 -o /tmp/ia-resp.json -w '%{http_code}' -X POST \
    -H "X-Veil-Token: ${TOKEN}" -H 'Content-Type: application/json' \
    -H "Idempotency-Key: ci-acceptance-mieru-idem" -d "$1" "${API}/api/inbounds"
}
idem_body='{"name":"ci-mieru-idem","protocol":"mieru","transport":"tcp","port":34447,"enabled":true,"password":"idem-pass"}'
first="$(idem_code "${idem_body}")"
second="$(idem_code "${idem_body}")"
[ "${first}" = "201" ] || ci_die "idempotent create: first HTTP ${first}: $(cat /tmp/ia-resp.json)"
[ "${second}" = "201" ] || ci_die "idempotent replay did not return the original 201 (got ${second})"

apply_panel
api_code GET /api/apply/state
cp /tmp/ia-resp.json "${CI_ARTIFACT_DIR}/apply-state-after-inbounds.json"
grep -q '"state":"synced"' "${CI_ARTIFACT_DIR}/apply-state-after-inbounds.json" \
  || ci_die "apply state did not reach synced: $(cat "${CI_ARTIFACT_DIR}/apply-state-after-inbounds.json")"

ci_step "rendered configs"
${SUDO} test -f /etc/veil/generated/hysteria2/ci-hy2.yaml || ci_die "hysteria2 config not rendered"
${SUDO} test -f /etc/veil/generated/mieru/server_config.json || ci_die "mieru config not rendered"
${SUDO} test -f /etc/veil/generated/olcrtc/ci-olc.yaml || ci_die "olcrtc config not rendered"

ci_step "protocol service lifecycle"
systemctl is-active --quiet veil-hysteria2@ci-hy2.service || ci_die "veil-hysteria2@ci-hy2 not active"
systemctl is-active --quiet veil-mieru.service || ci_die "veil-mieru not active"
# olcRTC needs an external conferencing provider at runtime; the contract is
# that the panel-generated config validates and the instance is enabled.
systemctl is-enabled --quiet veil-olcrtc@ci-olc.service || ci_die "veil-olcrtc@ci-olc not enabled"

ci_step "export/client-link shape"
api_code GET /api/client-links
cp /tmp/ia-resp.json "${CI_ARTIFACT_DIR}/client-links.json"
for proto in hysteria2 mieru olcrtc; do
  grep -q "\"${proto}\"" "${CI_ARTIFACT_DIR}/client-links.json" \
    || ci_die "client-links missing protocol ${proto}: $(cat "${CI_ARTIFACT_DIR}/client-links.json")"
done

# --- 5. Reinstall idempotency -------------------------------------------------
ci_step "idempotent reinstall (existing state reused)"
ci_run veil-reinstall ${SUDO} /usr/local/bin/veil install "${INSTALL_FLAGS[@]}"
grep -q "Reused the existing admin login" "${CI_ARTIFACT_DIR}/veil-reinstall.log" \
  || ci_die "reinstall did not reuse the existing state"
systemctl is-active --quiet veil.service || ci_die "veil.service not active after reinstall"
ci_run veil-status-after-reinstall ${SUDO} /usr/local/bin/veil status --json

# --- 6. Controlled ACME CA issuance (audit #304) -------------------------------
# Issue a REAL certificate against a pinned pebble test CA instead of public
# Let's Encrypt. VEIL_ACME_CA_URL switches the acme.sh --server value and
# VEIL_ACME_INSECURE skips TLS verification of pebble's self-signed endpoint.
ci_step "controlled ACME CA (pebble) real issuance"
pebble_bin="$(go env GOPATH)/bin/pebble"
if [ ! -x "${pebble_bin}" ]; then
  GOBIN="$(go env GOPATH)/bin" go install "github.com/letsencrypt/pebble/v2/cmd/pebble@${CI_PEBBLE_VERSION}"
fi
pebble_mod="$(go env GOMODCACHE)/github.com/letsencrypt/pebble/v2@${CI_PEBBLE_VERSION}"
(cd "${pebble_mod}" && PEBBLE_VA_NOSLEEP=1 "${pebble_bin}" -config test/config/pebble-config.json \
  > "${CI_ARTIFACT_DIR}/pebble.log" 2>&1 &)
pebble_up=1
for _ in $(seq 1 60); do
  if curl --http1.1 -sk --max-time 60 https://127.0.0.1:14000/dir | grep -q newOrder; then
    pebble_up=0
    break
  fi
  sleep 1
done
[ "${pebble_up}" -eq 0 ] || ci_die "pebble ACME directory did not come up (see pebble.log)"
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

# --- 7. Reboot persistence (smolvm system VM only) ---------------------------
if [ -n "${IA_PHASE_MARKER}" ]; then
  ci_log "smolvm system VM: staging post-reboot phase and rebooting"
  echo phase-b > "${IA_PHASE_MARKER}"
  sync
  systemctl reboot
  sleep 30
  ci_die "guest reboot did not take effect"
fi

uninstall_leg

ci_log "install-acceptance job passed"
