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

# --network host: build-time transport only (identical image content). The
# default bridge NAT is silently broken on some hosts (module downloads stall
# in LAST_ACK/Send-Q); host networking matches the daemon's own egress path.
ci_run image-check docker build --check .
ci_run image-build docker build --pull --network host \
  --build-arg "VERSION=${version}" \
  --build-arg "GO_GODEBUG=${CI_GO_GODEBUG}" \
  --build-arg "GO_GOPROXY=${CI_GO_GOPROXY}" \
  --tag veil:ci .
docker run --rm veil:ci version | tee "${CI_ARTIFACT_DIR}/image-version.txt" | grep -F "${version}"

ci_log "image-build job passed"
