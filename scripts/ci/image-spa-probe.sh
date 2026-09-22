#!/usr/bin/env bash
# scripts/ci/image-spa-probe.sh — assert a Veil OCI image actually serves the
# embedded panel SPA.
#
# `veil version` only proves the ldflags stamp — a stub/empty web/dist embed
# still greens it (#441). This probe runs the image, polls the loopback panel
# via docker exec, and requires the real SPA shell (id="root"). It is shared
# by the PR image-build job and the release docker-publish verify step so the
# shipped multi-arch image cannot drift from what PR CI proved (#681).
#
# Usage: image-spa-probe.sh <image-ref> [oci-platform] [artifact-dir]   e.g.
#   image-spa-probe.sh veil:ci "" .artifacts/ci
#   image-spa-probe.sh ghcr.io/org/veil:1.2.3 linux/arm64
# When artifact-dir is given, the probed index and failure-time container
# logs land there (image-index[-<arch>].html / image-panel[-<arch>].log) —
# same evidence the inline probe used to leave in CI_ARTIFACT_DIR.
set -euo pipefail

image="${1:?image ref required}"
platform="${2:-}"
artifact_dir="${3:-}"

arch_suffix="${platform:+-${platform##*/}}"
check_name="veil-spa-probe-$$"
panel_log=/dev/stderr
if [ -n "${artifact_dir}" ]; then
  mkdir -p "${artifact_dir}"
  index_file="${artifact_dir}/image-index${arch_suffix}.html"
  panel_log="${artifact_dir}/image-panel${arch_suffix}.log"
else
  index_file="$(mktemp)"
fi
cleanup() {
  docker rm -f "${check_name}" >/dev/null 2>&1 || true
  if [ -z "${artifact_dir}" ]; then
    rm -f "${index_file}"
  fi
}
trap cleanup EXIT

platform_args=()
if [ -n "${platform}" ]; then
  platform_args=(--platform "${platform}")
fi

# Serve on container loopback and probe via docker exec: a 0.0.0.0 listener
# would trip the public-exposure policy (token + session users + TLS) on a
# fresh state, and port publishing races container exit.
docker run -d --name "${check_name}" "${platform_args[@]}" \
  "${image}" serve --listen 127.0.0.1:2096 >/dev/null

spa_up=1
for _ in $(seq 1 60); do
  if docker exec "${check_name}" wget -q -O- "http://127.0.0.1:2096/" \
       >"${index_file}" 2>/dev/null \
     && grep -q 'id="root"' "${index_file}"; then
    spa_up=0
    break
  fi
  # Container exited? Fail now with logs instead of polling a dead box.
  if [ "$(docker inspect -f '{{.State.Running}}' "${check_name}" 2>/dev/null)" != "true" ]; then
    docker logs "${check_name}" >"${panel_log}" 2>&1 || true
    echo "image ${image}: panel container exited before serving" >&2
    exit 1
  fi
  sleep 1
done
if [ "${spa_up}" -ne 0 ]; then
  docker logs "${check_name}" >"${panel_log}" 2>&1 || true
  echo "image ${image} does not serve the embedded panel SPA" >&2
  exit 1
fi
echo "image ${image}${platform:+ (${platform})} serves the embedded panel SPA"
