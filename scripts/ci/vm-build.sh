#!/usr/bin/env bash
# scripts/ci/vm-build.sh — builds the CI OCI images (veil-ci-base/-browser/-system)
# from ci/vm/Containerfile, keyed by a content hash of every build input.
# `make ci` never rebuilds unchanged images; `make ci-image CI_CLEAN=1` forces
# a clean rebuild.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

VM_DIR="${CI_ROOT}/ci/vm"

command -v docker >/dev/null 2>&1 || ci_die "docker is required to build CI images (image work is delegated to container tooling, same as smolvm)"

# --- Image key: sha256 over every input that affects the image ---------------
ci_image_key() {
  {
    cat "${VM_DIR}/Containerfile"
    cat "${VM_DIR}/image.lock"
    cat "${VM_DIR}/packages.lock"
    cat "${CI_SCRIPTS_DIR}/versions.sh"
    cat "${VM_DIR}/entrypoint.sh"
    cat "${VM_DIR}/guest-run.sh"
    cat "${VM_DIR}/prepare-workspace.sh"
    find "${VM_DIR}/systemd" -type f -exec cat {} + 2>/dev/null || true
  } | sha256sum | cut -c1-16
}

KEY="$(ci_image_key)"
echo "${KEY}" > "${CI_ARTIFACT_DIR}/image-key.txt"

targets=(base browser system)
declare -A TAGS=(
  [base]="veil-ci-base:${KEY}"
  [browser]="veil-ci-browser:${KEY}"
  [system]="veil-ci-system:${KEY}"
)

# shellcheck disable=SC1091  # image.lock is a KEY=VALUE lockfile, not an input script
. "${VM_DIR}/image.lock"
if [ "${CI_UBUNTU_BASE}" != "ubuntu:24.04@${UBUNTU_24_04_DIGEST}" ]; then
  printf '[ci] ERROR: Ubuntu base drift between versions.sh and image.lock\n' >&2
  exit 1
fi

build_target() {
  local target="$1"
  local tag="${TAGS[${target}]}"
  if [ "${CI_CLEAN:-0}" != "1" ] && docker image inspect "${tag}" >/dev/null 2>&1; then
    ci_log "image ${tag} up-to-date (inputs unchanged)"
    return 0
  fi
  ci_step "building ${tag}"
  local no_cache=()
  [ "${CI_CLEAN:-0}" = "1" ] && no_cache=(--no-cache)
  # --network host: build-time transport only (identical image content, and
  # the image key inputs do not include the network mode). The default bridge
  # NAT is silently broken on some hosts (downloads stall with growing Send-Q
  # and LAST_ACK connections); host networking matches the daemon's egress.
  docker build "${no_cache[@]}" \
    --network host \
    --target "${target}" \
    --tag "${tag}" \
    --build-arg "UBUNTU_BASE=ubuntu:24.04@${UBUNTU_24_04_DIGEST}" \
    --build-arg "CI_GO_VERSION=${CI_GO_VERSION}" \
    --build-arg "CI_GO_TARBALL_SHA256=${CI_GO_TARBALL_SHA256}" \
    --build-arg "CI_GO_GODEBUG=${CI_GO_GODEBUG}" \
    --build-arg "CI_GO_GOPROXY=${CI_GO_GOPROXY}" \
    --build-arg "CI_NODE_VERSION=${CI_NODE_VERSION}" \
    --build-arg "CI_NODE_TARBALL_SHA256=${CI_NODE_TARBALL_SHA256}" \
    --build-arg "CI_NPM_VERSION=${CI_NPM_VERSION}" \
    --build-arg "CI_PNPM_VERSION=${CI_PNPM_VERSION}" \
    --build-arg "CI_STATICCHECK_VERSION=${CI_STATICCHECK_VERSION}" \
    --build-arg "CI_GOVULNCHECK_VERSION=${CI_GOVULNCHECK_VERSION}" \
    --build-arg "CI_NFPM_VERSION=${CI_NFPM_VERSION}" \
    --build-arg "CI_REDOCLY_VERSION=${CI_REDOCLY_VERSION}" \
    --build-arg "CI_DOCKER_CLI_VERSION=${CI_DOCKER_CLI_VERSION}" \
    --build-arg "CI_DOCKER_CLI_SHA256=${CI_DOCKER_CLI_SHA256}" \
    --build-arg "CI_DOCKER_BUILDX_VERSION=${CI_DOCKER_BUILDX_VERSION}" \
    --build-arg "CI_DOCKER_BUILDX_SHA256=${CI_DOCKER_BUILDX_SHA256}" \
    --build-arg "CI_PLAYWRIGHT_VERSION=${CI_PLAYWRIGHT_VERSION}" \
    --build-arg "CI_CADDY_VERSION=${CI_CADDY_VERSION}" \
    --build-arg "CI_FORWARDPROXY_VERSION=${CI_FORWARDPROXY_VERSION}" \
    --build-arg "CI_HYSTERIA_TAG=${CI_HYSTERIA_TAG}" \
    --build-arg "CI_HYSTERIA_ASSET=${CI_HYSTERIA_ASSET}" \
    --build-arg "CI_HYSTERIA_SHA256=${CI_HYSTERIA_SHA256}" \
    --build-arg "CI_MITA_TAG=${CI_MITA_TAG}" \
    --build-arg "CI_MITA_ASSET=${CI_MITA_ASSET}" \
    --build-arg "CI_MITA_SHA256=${CI_MITA_SHA256}" \
    --build-arg "CI_MIERU_CLIENT_TAG=${CI_MIERU_CLIENT_TAG}" \
    --build-arg "CI_MIERU_CLIENT_ASSET=${CI_MIERU_CLIENT_ASSET}" \
    --build-arg "CI_MIERU_CLIENT_SHA256=${CI_MIERU_CLIENT_SHA256}" \
    --build-arg "CI_NAIVE_CLIENT_TAG=${CI_NAIVE_CLIENT_TAG}" \
    --build-arg "CI_NAIVE_CLIENT_ASSET=${CI_NAIVE_CLIENT_ASSET}" \
    --build-arg "CI_NAIVE_CLIENT_SHA256=${CI_NAIVE_CLIENT_SHA256}" \
    --build-arg "CI_SINGBOX_TAG=${CI_SINGBOX_TAG}" \
    --build-arg "CI_SINGBOX_ASSET=${CI_SINGBOX_ASSET}" \
    --build-arg "CI_SINGBOX_SHA256=${CI_SINGBOX_SHA256}" \
    -f "${VM_DIR}/Containerfile" \
    "${VM_DIR}"
}

if [ "${1:-}" = "--check" ]; then
  missing=0
  for t in "${targets[@]}"; do
    docker image inspect "${TAGS[$t]}" >/dev/null 2>&1 || missing=1
  done
  exit "${missing}"
fi

ci_timer_start
for t in "${targets[@]}"; do
  build_target "${t}"
done
ci_timer_stop "ci-image-build"

# Metadata + sizes.
{
  echo "image key: ${KEY}"
  for t in "${targets[@]}"; do
    size="$(docker image inspect "${TAGS[$t]}" --format '{{.Size}}')"
    mib=$(( size / 1048576 ))
    printf 'veil-ci-%s\t%s\t%d bytes (%d MiB)\n' "${t}" "${TAGS[$t]}" "${size}" "${mib}"
  done
} | tee "${CI_ARTIFACT_DIR}/image-metadata.txt"

ci_log "CI images ready (key ${KEY})"
