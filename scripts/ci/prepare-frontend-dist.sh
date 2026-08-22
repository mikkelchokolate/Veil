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
if [ ! -f "${artifact_dir}/dist/index.html" ]; then
  ci_die "frontend artifact is missing dist/index.html: ${artifact_dir}/dist"
fi

rm -rf "${CI_ROOT}/web/dist"
mkdir -p "${CI_ROOT}/web/dist"
cp -a "${artifact_dir}/dist/." "${CI_ROOT}/web/dist/"
ci_log "restored frontend dist for source ${source_sha}"
