#!/usr/bin/env bash
# scripts/ci/image-build.sh — OCI/Docker image build CI job.
# Builds the Veil image with an injected version and verifies `veil version`
# reports it. Uses the Docker daemon on GitHub-hosted runners and in the
# explicit local host-Docker phase; it never runs inside the daemonless smolvm
# guest.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

# CI_SOURCE_SHA carries the ORIGINAL source commit (the guest repo is a fresh
# git-init snapshot whose HEAD does not identify the source tree).
version="ci-${CI_SOURCE_SHA:-$(git rev-parse HEAD 2>/dev/null || echo local)}"

if ! docker info >/dev/null 2>&1; then
  ci_die "docker daemon unavailable — image-build requires a working OCI build backend"
fi

# --check must see the same build-arg surface as the real build — ARG issues
# that only appear with VERSION/GO_GODEBUG/GO_GOPROXY set otherwise slip
# past the check (#436).
ci_run image-check docker build --check \
  --build-arg "VERSION=${version}" \
  --build-arg "GO_GODEBUG=${CI_GO_GODEBUG}" \
  --build-arg "GO_GOPROXY=${CI_GO_GOPROXY}" \
  .
# CI_CLEAN=1 is the documented cold path: pass --no-cache so layer reuse does
# not silently green a stale build (#440).
no_cache=()
[ "${CI_CLEAN:-0}" = "1" ] && no_cache=(--no-cache)
ci_run image-build docker build --pull "${no_cache[@]}" \
  --build-arg "VERSION=${version}" \
  --build-arg "GO_GODEBUG=${CI_GO_GODEBUG}" \
  --build-arg "GO_GOPROXY=${CI_GO_GOPROXY}" \
  --tag veil:ci .
docker run --rm veil:ci version | tee "${CI_ARTIFACT_DIR}/image-version.txt" | grep -F "${version}"

# `veil version` only proves the ldflags stamp — a stub/empty web/dist embed
# still greens it (#441). Run the image and assert the panel actually serves
# the real SPA shell over its public HTTP route.
check_name="veil-ci-imagecheck-$$"
cleanup_image_check() {
  docker rm -f "${check_name}" >/dev/null 2>&1 || true
}
trap cleanup_image_check EXIT
# serve requires --auth-token for non-loopback listeners; without it the
# container exits immediately and no port is ever published.
docker run -d --name "${check_name}" -p 127.0.0.1::2096 \
  veil:ci serve --listen 0.0.0.0:2096 --auth-token ci-image-check-token >/dev/null
image_port="$(docker port "${check_name}" 2096/tcp | head -1 | rev | cut -d: -f1 | rev)"
if [ -z "${image_port}" ]; then
  docker logs "${check_name}" >"${CI_ARTIFACT_DIR}/image-panel.log" 2>&1 || true
  ci_die "could not resolve published port for ${check_name} — container exited early (see image-panel.log)"
fi
spa_up=1
for _ in $(seq 1 60); do
  if curl -sfL --max-time 10 "http://127.0.0.1:${image_port}/" -o "${CI_ARTIFACT_DIR}/image-index.html" 2>/dev/null \
     && grep -q 'id="root"' "${CI_ARTIFACT_DIR}/image-index.html"; then
    spa_up=0
    break
  fi
  sleep 1
done
if [ "${spa_up}" -ne 0 ]; then
  docker logs "${check_name}" >"${CI_ARTIFACT_DIR}/image-panel.log" 2>&1 || true
  ci_die "built image does not serve the embedded panel SPA (see image-panel.log)"
fi
docker rm -f "${check_name}" >/dev/null 2>&1 || true
trap - EXIT

ci_log "image-build job passed"
