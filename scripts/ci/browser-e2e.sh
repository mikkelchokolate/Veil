#!/usr/bin/env bash
# scripts/ci/browser-e2e.sh — browser E2E CI job (Playwright against a real panel).
# Mirrors the former `browser-e2e` workflow job: three panel instances
# (standard, WebBasePath, helper-backed production layout) + pinned Playwright.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

if [ "${CI_IN_GUEST:-}" != "1" ] && {
  [ -e /etc/veil/state.key ] || [ -e /var/lib/veil/state.json ] || [ -S /run/veil/helper.sock ];
}; then
  ci_die "refusing browser E2E on a host with production Veil sentinels; run it through make ci/ci-full"
fi

if [ "$(id -u)" -eq 0 ]; then SUDO=""; else SUDO="sudo"; fi
run_as_veil() {
  if [ "$(id -u)" -eq 0 ]; then
    runuser -u veil -- "$@"
  else
    sudo -u veil -- "$@"
  fi
}
WORK="${CI_ARTIFACT_DIR}/browser-e2e"
mkdir -p "${WORK}"

ci_step "web/dist (embedded into the binary)"
(cd web && pnpm install --frozen-lockfile && pnpm build)

ci_step "browser-mode unit tests (Chromium)"
(cd web && pnpm test:browser)

ci_step "build browser-test binary"
mkdir -p dist
go build -trimpath -ldflags "-s -w -X main.version=browser-e2e" -o dist/veil ./cmd/veil

ci_step "pinned Playwright (${CI_PLAYWRIGHT_VERSION})"
(cd test/browser && npm ci --ignore-scripts)
# Browsers are baked into the CI image (PLAYWRIGHT_BROWSERS_PATH); never
# provision at run time — fail loudly if the image is missing them.
if ! (cd test/browser && npx playwright install --dry-run chromium >/dev/null 2>&1); then
  ci_die "chromium not present in image — rebuild veil-ci-browser (no runtime provisioning allowed)"
fi

wait_health() { # <url> <log>
  local url="$1" log="$2"
  for _ in $(seq 1 60); do
    if curl --fail --silent "${url}" >/dev/null; then return 0; fi
    sleep 1
  done
  cat "${log}" >&2 || true
  return 1
}

PIDS=()
cleanup_panels() {
  for pid in "${PIDS[@]:-}"; do
    if [ -n "${pid}" ]; then ${SUDO} kill "${pid}" 2>/dev/null || true; fi
  done
}
trap cleanup_panels EXIT

start_panel() { # <name> <listen-args...>
	local name="$1"; shift
	local root="${WORK}/${name}"
	mkdir -p "${root}"
	${SUDO} mkdir -p "${root}/state" "${root}/etc" "${root}/apply" "${root}/live"
	${SUDO} chown -R veil:veil "${root}/state" "${root}/etc" "${root}/apply" "${root}/live"
	run_as_veil env \
		VEIL_STATE_PATH="${root}/state/state.json" \
		VEIL_KEY_PATH="${root}/etc/state.key" \
		VEIL_APPLY_ROOT="${root}/apply" \
		VEIL_LIVE_ROOT="${root}/live" \
		VEIL_API_TOKEN=browser-e2e-token \
		./dist/veil admin set --username browser-admin --password 'Browser-E2E-Password-123!' \
			--role admin --state "${root}/state/state.json" --key-path "${root}/etc/state.key"
	run_as_veil env \
		VEIL_STATE_PATH="${root}/state/state.json" \
		VEIL_KEY_PATH="${root}/etc/state.key" \
		VEIL_APPLY_ROOT="${root}/apply" \
		VEIL_LIVE_ROOT="${root}/live" \
		VEIL_HELPER_SOCKET=/run/veil/helper.sock \
		VEIL_API_TOKEN=browser-e2e-token \
		./dist/veil serve "$@" >"${root}/panel.log" 2>&1 &
	PIDS+=($!)
}

ci_step "start root helper and production-layout state"
${SUDO} useradd --system --home-dir /nonexistent --no-create-home --shell /usr/sbin/nologin veil 2>/dev/null || true
${SUDO} mkdir -p /var/lib/veil/backups /etc/veil /run/veil
${SUDO} chown -R veil:veil /var/lib/veil /etc/veil
openssl rand -hex 32 | ${SUDO} tee /etc/veil/backup.passphrase >/dev/null
${SUDO} chown veil:veil /etc/veil/backup.passphrase
${SUDO} chmod 600 /etc/veil/backup.passphrase
run_as_veil ./dist/veil admin set --username browser-admin --password 'Browser-E2E-Password-123!' \
	--role admin --state /var/lib/veil/state.json --key-path /etc/veil/state.key
${SUDO} ./dist/veil helper serve --socket /run/veil/helper.sock >"${WORK}/helper.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 1 30); do [ -S /run/veil/helper.sock ] && break; sleep 1; done
if [ ! -S /run/veil/helper.sock ]; then
	cat "${WORK}/helper.log" >&2 || true
	ci_die "root helper socket did not appear"
fi
${SUDO} chgrp veil /run/veil/helper.sock

ci_step "start authenticated panel (:2096)"
start_panel main --listen 127.0.0.1:2096
wait_health http://127.0.0.1:2096/healthz "${WORK}/main/panel.log"

ci_step "start WebBasePath panel (:2097, /e2e-base-x9/)"
start_panel pathed --listen 127.0.0.1:2097 --web-base-path /e2e-base-x9/
wait_health http://127.0.0.1:2097/e2e-base-x9/healthz "${WORK}/pathed/panel.log"

ci_step "start helper-backed panel (:2098, production layout)"
# Backup operations are privileged: reproduce the production layout faithfully
# (veil system user, /var/lib/veil state, root helper on /run/veil/helper.sock).
run_as_veil env VEIL_STATE_PATH=/var/lib/veil/state.json VEIL_KEY_PATH=/etc/veil/state.key \
  VEIL_APPLY_ROOT=/var/lib/veil/apply VEIL_LIVE_ROOT=/var/lib/veil/live \
  VEIL_API_TOKEN=browser-e2e-token ./dist/veil serve --listen 127.0.0.1:2098 >"${WORK}/backup-panel.log" 2>&1 &
PIDS+=($!)
wait_health http://127.0.0.1:2098/healthz "${WORK}/backup-panel.log"
if ! curl --fail --silent -X POST -H 'X-Veil-Token: browser-e2e-token' \
    -H 'Content-Type: application/json' -d '{}' http://127.0.0.1:2098/api/backups >/dev/null; then
  echo "helper-backed backup probe failed" >&2
  cat "${WORK}/helper.log" >&2 || true
  cat "${WORK}/backup-panel.log" >&2 || true
  exit 1
fi

ci_step "playwright"
(
  cd test/browser
  export VEIL_BROWSER_BASE_URL=http://127.0.0.1:2096
  export VEIL_BROWSER_BASE_URL_PATHED=http://127.0.0.1:2097/e2e-base-x9/
  export VEIL_BROWSER_BACKUP_URL=http://127.0.0.1:2098
  export VEIL_BROWSER_USERNAME=browser-admin
  export VEIL_BROWSER_PASSWORD='Browser-E2E-Password-123!'
  export VEIL_BROWSER_API_TOKEN=browser-e2e-token
  node_modules/.bin/playwright test
)

# Preserve diagnostics.
cp -rf test/browser/playwright-report "${WORK}/" 2>/dev/null || true
cp -rf test/browser/test-results "${WORK}/" 2>/dev/null || true

ci_log "browser-e2e job passed"
