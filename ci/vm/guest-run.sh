#!/usr/bin/env bash
# ci/vm/guest-run.sh — runs INSIDE the CI guest. Prepares the workspace from
# /exchange/snapshot.tar and executes one CI job script with the correct user,
# then mirrors artifacts back to /exchange/artifacts (success AND failure).
#
# Usage: guest-run.sh <job> [job-args...]
#   <job> may be a scripts/ci/*.sh name (frontend, test, ...) or one of the
#   composite drivers: standard, full, stress.
set -uo pipefail
# NOTE: no -e — we must capture and propagate the job's exit code ourselves.

JOB="${1:?usage: guest-run.sh <job> [args...]}"
shift || true

EXCHANGE="${CI_EXCHANGE_DIR:-/exchange}"
WORKSPACE="${CI_WORKSPACE_OVERRIDE:-/workspace/veil}"
ARTIFACTS_GUEST="/workspace/artifacts"

mkdir -p "${ARTIFACTS_GUEST}"

finish() {
  rc=$?
  # Artifacts out on every exit path.
  if [ -d "${ARTIFACTS_GUEST}" ] && [ -d "${EXCHANGE}/artifacts" ]; then
    cp -rf "${ARTIFACTS_GUEST}/." "${EXCHANGE}/artifacts/" 2>/dev/null || true
  fi
  exit "${rc}"
}
trap finish EXIT

echo "[guest] job=${JOB} pid1=$(cat /proc/1/comm) user=$(id -un)"

/opt/ci/prepare-workspace.sh "${EXCHANGE}/snapshot.tar"
# Cache mounts may have created root-owned parent dirs; the job user needs a
# writable HOME (staticcheck cache, npm cache, GONOSUMDB dir, pnpm store root).
mkdir -p /home/ci/.cache /home/ci/go/pkg /home/ci/.local/share/pnpm
chown ci:ci /home/ci /home/ci/.cache /home/ci/go /home/ci/go/pkg \
  /home/ci/.local /home/ci/.local/share /home/ci/.local/share/pnpm 2>/dev/null || true

cd "${WORKSPACE}"

# Jobs that must run as root (system integration) vs as the unprivileged ci
# user. Root only where the production layout requires it.
ROOT_JOBS=" privilege-boundary e2e package-smoke image-build "
JOB_USER="ci"
case "${ROOT_JOBS}" in
  *" ${JOB} "*) JOB_USER="root" ;;
esac
# Composite drivers: run as root only when they include system jobs. `standard`
# is always unprivileged (its OCI-build validation is dispatched separately by
# run-job.sh as JOB=image-build in the system guest, which IS root). Running
# the base phase as root makes git refuse the ci-owned workspace repo
# ("detected dubious ownership" → "Not a git repository") and defeats the
# unit-test privilege boundary.
if [ "${JOB}" = "standard" ]; then
  JOB_USER="ci"
fi
if [ "${JOB}" = "full" ]; then
  case "${CI_FULL_PHASE:-all}" in
    system) JOB_USER="root" ;;
    all)    JOB_USER="root" ;;  # single-VM fallback: mixed; system steps sudo internally
    *)      JOB_USER="ci" ;;
  esac
fi

export CI_ARTIFACT_DIR="${ARTIFACTS_GUEST}"
export CI_REPO_ROOT="${WORKSPACE}"
export CI_EXCHANGE_DIR="${EXCHANGE}"

run_as() {
  local user="$1"; shift
  if [ "${user}" = "root" ]; then
    "$@"
  else
    # Preserve CI env for the unprivileged user; HOME must be writable.
    su -s /bin/bash ci -c "$(printf '%q ' env \
      CI_ARTIFACT_DIR="${CI_ARTIFACT_DIR}" \
      CI_REPO_ROOT="${CI_REPO_ROOT}" \
      CI_EXCHANGE_DIR="${CI_EXCHANGE_DIR}" \
      CI_FULL_PHASE="${CI_FULL_PHASE:-}" \
      CI_SOURCE_SHA="${CI_SOURCE_SHA:-}" \
      VEIL_CI_RUNTIME_DIR="${VEIL_CI_RUNTIME_DIR:-/opt/veil-runtime}" \
      PATH="/usr/local/go/bin:/usr/local/node/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
      HOME=/home/ci \
      "$@")"
  fi
}

# Ensure the ci user owns artifact dirs even when jobs run as root.
chown -R ci:ci "${ARTIFACTS_GUEST}" /workspace 2>/dev/null || true

rc=0
run_as "${JOB_USER}" bash "${WORKSPACE}/scripts/ci/${JOB}.sh" "$@" || rc=$?

echo "[guest] job=${JOB} exit=${rc}"
exit "${rc}"
