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
# the real SPA shell. Serve on container loopback and probe via docker exec:
# a 0.0.0.0 listener would trip the public-exposure policy (token + session
# users + TLS) on a fresh state, and port publishing races container exit.
check_name="veil-ci-imagecheck-$$"
cleanup_image_check() {
  docker rm -f "${check_name}" >/dev/null 2>&1 || true
}
trap cleanup_image_check EXIT
docker run -d --name "${check_name}" \
  veil:ci serve --listen 127.0.0.1:2096 >/dev/null
spa_up=1
for _ in $(seq 1 60); do
  if docker exec "${check_name}" wget -q -O- "http://127.0.0.1:2096/" \
       >"${CI_ARTIFACT_DIR}/image-index.html" 2>/dev/null \
     && grep -q 'id="root"' "${CI_ARTIFACT_DIR}/image-index.html"; then
    spa_up=0
    break
  fi
  # Container exited? Fail now with logs instead of polling a dead box.
  if [ "$(docker inspect -f '{{.State.Running}}' "${check_name}" 2>/dev/null)" != "true" ]; then
    docker logs "${check_name}" >"${CI_ARTIFACT_DIR}/image-panel.log" 2>&1 || true
    ci_die "image panel container exited before serving (see image-panel.log)"
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
