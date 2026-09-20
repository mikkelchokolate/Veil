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
  grep -q "${port_spec}" "${CI_ARTIFACT_DIR}/ufw-status-after-inbounds.txt" \
    || ci_die "ufw has no allow rule for inbound ${port_spec}: $(cat "${CI_ARTIFACT_DIR}/ufw-status-after-inbounds.txt")"
done

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
pebble_bin="$(go env GOPATH)/bin/pebble"
if [ ! -x "${pebble_bin}" ]; then
  GOBIN="$(go env GOPATH)/bin" go install "github.com/letsencrypt/pebble/v2/cmd/pebble@${CI_PEBBLE_VERSION}"
fi
pebble_mod="$(go env GOMODCACHE)/github.com/letsencrypt/pebble/v2@${CI_PEBBLE_VERSION}"
# Patch tlsPort to 443 so the pebble VA validates tls-alpn-01 against Caddy's
# real public listener in the caddy-mode leg below (the stock test config
# targets 5001, which nothing here listens on).
sed 's/"tlsPort": 5001/"tlsPort": 443/' "${pebble_mod}/test/config/pebble-config.json"   > "${CI_ARTIFACT_DIR}/pebble-config.json"
(cd "${pebble_mod}" && PEBBLE_VA_NOSLEEP=1 "${pebble_bin}" -config "${CI_ARTIFACT_DIR}/pebble-config.json"   > "${CI_ARTIFACT_DIR}/pebble.log" 2>&1 &)
pebble_up=1
for _ in $(seq 1 60); do
  if grep -q newOrder < <(curl --http1.1 -sk --max-time 60 https://127.0.0.1:14000/dir); then
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
# The ACME reinstall restarted the panel; the protocol units from the
# first-inbound leg must still be standing as well.
assert_protocol_units "after controlled-CA reinstall"

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
# reverse-proxy path on the same host: a real caddy-mode install, real ACME
# issuance for the panel domain via caddy's tls-alpn-01 against the pebble CA,
# and an HTTPS request through the public :443 route to the panel — the
# failure mode where install reports success but nothing serves the URL.
# Scope honesty: this leg does not exercise naiveproxy; its data-path coverage
# is owned by the e2e job (TestRequiredNaiveProxyDataPath), see issue #351.
ci_step "caddy-mode install: real ACME issuance + public route"
CADDY_CI_DOMAIN="veil-ci.test"
# Both the pebble VA (its system resolver honours /etc/hosts) and the local
# route probe must resolve the panel domain to this host.
grep -q " ${CADDY_CI_DOMAIN}" /etc/hosts || echo "127.0.0.1 ${CADDY_CI_DOMAIN}" | ${SUDO} tee -a /etc/hosts >/dev/null
# Trust anchor for Caddy's ACME client; readable by the veil user through the
# unit's ReadOnlyPaths=/etc/veil.
${SUDO} install -m 0644 "${pebble_mod}/test/certs/pebble.minica.pem" /etc/veil/acme-root.pem
ci_run veil-install-caddy ${SUDO} env   VEIL_ACME_CA_URL="https://127.0.0.1:14000/dir" VEIL_ACME_CA_ROOT=/etc/veil/acme-root.pem   /usr/local/bin/veil install --yes --panel-access caddy --domain "${CADDY_CI_DOMAIN}" --email "ci@veil-ci.test"
systemctl is-active --quiet veil-caddy.service || ci_die "veil-caddy.service is not active after caddy-mode install"
systemctl is-enabled --quiet veil-caddy.service || ci_die "veil-caddy.service is not enabled"
# Re-read the base path THIS install wrote — the probe must verify the URL
# the panel actually serves now, not the value cached during the direct-mode
# leg (reinstall reuses state today, but the contract is the env on disk).
BASE_PATH="$(${SUDO} grep '^VEIL_WEB_BASE_PATH=' /etc/veil/veil.env | cut -d= -f2- | tr -d '[:space:]' || true)"
[ -n "${BASE_PATH}" ] || ci_die "VEIL_WEB_BASE_PATH missing from /etc/veil/veil.env after caddy-mode install"
# tls-alpn-01 issuance completes on the first TLS handshake; poll the public
# route until caddy serves a pebble-issued certificate and proxies the panel.
# --cacert success proves the cert chain; any HTTP status other than the
# caddy-generated fallbacks (000 connect failure, 404 route miss, 502 backend
# down) proves the request reached the panel through the public route.
caddy_route=1
route_code=000
# The chain caddy serves is issued by the root pebble GENERATES at startup,
# not by the static minica that signs pebble's API TLS certificate — fetch
# the live issuing root from pebble's management interface.
pebble_issuing_root="${CI_ARTIFACT_DIR}/pebble-issuing-root.pem"
curl -sf --cacert "${pebble_mod}/test/certs/pebble.minica.pem"   "https://127.0.0.1:15000/roots/0" -o "${pebble_issuing_root}"   || ci_die "could not fetch pebble issuing root from the management API"
for _ in $(seq 1 60); do
  route_code="$(curl --http1.1 -s --cacert "${pebble_issuing_root}"     --resolve "${CADDY_CI_DOMAIN}:443:127.0.0.1" --max-time 15     -o /tmp/ia-caddy-route.out -w '%{http_code}' "https://${CADDY_CI_DOMAIN}${BASE_PATH}" 2>/tmp/ia-caddy-route.err || true)"
  case "${route_code}" in 000|404|502) ;; *) caddy_route=0; break ;; esac
  sleep 2
done
cp /tmp/ia-caddy-route.out "${CI_ARTIFACT_DIR}/caddy-route.out" 2>/dev/null || true
cp /tmp/ia-caddy-route.err "${CI_ARTIFACT_DIR}/caddy-route.err" 2>/dev/null || true
[ "${caddy_route}" -eq 0 ] || ci_die "public caddy route https://${CADDY_CI_DOMAIN}${BASE_PATH} did not serve the panel (last HTTP ${route_code})"
# --cacert proved the chain verifies; also assert the served certificate's
# issuer explicitly, at the same strength as the direct IP-cert leg above.
openssl s_client -connect "127.0.0.1:443" -servername "${CADDY_CI_DOMAIN}" </dev/null 2>/dev/null \
  | openssl x509 -noout -issuer | tee "${CI_ARTIFACT_DIR}/caddy-served-issuer.txt" || true
grep -qi "pebble" "${CI_ARTIFACT_DIR}/caddy-served-issuer.txt" \
  || ci_die "public route is not serving a pebble-issued certificate: $(cat "${CI_ARTIFACT_DIR}/caddy-served-issuer.txt")"

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
id -nG veil | tr ' ' '\n' | grep -qx veil-proxy || ci_die "veil not in veil-proxy group"

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
ci_run veil-uninstall-leftover ${SUDO} bash scripts/uninstall.sh --yes
${SUDO} test ! -e /etc/veil || ci_die "/etc/veil left after leftover uninstall"
${SUDO} test ! -e /var/lib/veil || ci_die "/var/lib/veil left after leftover uninstall"
bash scripts/uninstall.sh --yes 2>&1 | tee "${CI_ARTIFACT_DIR}/uninstall-second-pass.log"
grep -q "Nothing to uninstall" "${CI_ARTIFACT_DIR}/uninstall-second-pass.log" \
  || ci_die "second uninstall pass still finds leftover state"

ci_log "install-acceptance job passed"
