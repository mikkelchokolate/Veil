#!/usr/bin/env bash
# Restore a frontend build artifact only when it was built from this exact HEAD.
# Without CI_FRONTEND_DIST_ARTIFACT_DIR, preserve local/VM behavior by building.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"
artifact_dir="${CI_FRONTEND_DIST_ARTIFACT_DIR:-}"
if [ -z "${artifact_dir}" ]; then
  ci_run frontend-install-build bash -c 'cd web && pnpm install --frozen-lockfile && pnpm build'
  # A green build that produced a stub/empty dist must not embed — require
  # the same shape as the artifact path below (#413).
  [ -s "${CI_ROOT}/web/dist/index.html" ] \
    || ci_die "local frontend build produced no web/dist/index.html"
  find "${CI_ROOT}/web/dist" -type f -name '*.js' | grep -q . \
    || ci_die "local frontend build produced no JavaScript bundle (stub SPA)"
  exit 0
fi

case "${artifact_dir}" in
  /*) ;;
  *) artifact_dir="${CI_ROOT}/${artifact_dir}" ;;
esac
manifest="${artifact_dir}/source.sha"
source_sha="$(git rev-parse HEAD)"
if [ ! -f "${manifest}" ]; then
  ci_die "frontend artifact manifest is missing: ${manifest}"
fi
artifact_sha="$(tr -d '[:space:]' < "${manifest}")"
if [ "${artifact_sha}" != "${source_sha}" ]; then
  ci_die "frontend artifact SHA ${artifact_sha} does not match source HEAD ${source_sha}"
fi
# dist.sha256 binds CONTENT, not just the commit (#413, #414): recompute the
# exact file set + hashes — a stub SPA, truncated upload, or a dist rebuilt
# from different inputs cannot reuse this artifact's identity.
dist_manifest="${artifact_dir}/dist.sha256"
[ -f "${dist_manifest}" ] || ci_die "frontend artifact is missing the dist.sha256 content manifest"
(cd "${artifact_dir}/dist" && find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) \
  > "${CI_ARTIFACT_DIR}/dist.sha256.verify" \
  || ci_die "could not hash frontend artifact contents"
if ! cmp -s "${dist_manifest}" "${CI_ARTIFACT_DIR}/dist.sha256.verify"; then
  diff "${dist_manifest}" "${CI_ARTIFACT_DIR}/dist.sha256.verify" >&2 || true
  ci_die "frontend artifact contents do not match dist.sha256"
fi
# Require the shape of a real Vite build, not just any index.html: a non-empty
# document plus at least one JavaScript bundle (#413).
[ -s "${artifact_dir}/dist/index.html" ] \
  || ci_die "frontend artifact dist/index.html is missing or empty: ${artifact_dir}/dist"
find "${artifact_dir}/dist" -type f -name '*.js' | grep -q . \
  || ci_die "frontend artifact contains no JavaScript bundle (stub SPA): ${artifact_dir}/dist"

rm -rf "${CI_ROOT}/web/dist"
mkdir -p "${CI_ROOT}/web/dist"
cp -a "${artifact_dir}/dist/." "${CI_ROOT}/web/dist/"
ci_log "restored frontend dist for source ${source_sha}"
