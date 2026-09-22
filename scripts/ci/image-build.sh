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
# still greens it (#441). The shared probe runs the image and asserts the
# panel actually serves the real SPA shell; release.yml reuses the same probe
# on the pushed multi-arch tags (#681).
ci_run image-spa-probe bash "${CI_SCRIPTS_DIR}/image-spa-probe.sh" veil:ci "" "${CI_ARTIFACT_DIR}"

ci_log "image-build job passed"
