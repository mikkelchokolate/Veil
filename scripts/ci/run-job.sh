#!/usr/bin/env bash
# scripts/ci/run-job.sh — maps a logical CI job to its image and executes it in
# an isolated execution backend. Used by `make ci`, `make ci-full`, `make ci-pr`,
# and `make ci-job`.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

JOB="${1:?usage: run-job.sh <job> [args...]}"
shift || true

declare -A JOB_IMAGE=(
  [frontend]=base
  [test]=base
  [lint]=base
  [browser-e2e]=browser
  [privilege-boundary]=system
  [e2e]=system
  [package-smoke]=system
  [image-build]=system
  [standard]=base
  [full]=system
  [stress]=base
)

image="${JOB_IMAGE[${JOB}]:-}"
if [ -z "${image}" ]; then
  ci_die "unknown CI job '${JOB}'. Valid jobs: $(printf '%s ' "${!JOB_IMAGE[@]}" | tr ' ' '\n' | sort | tr '\n' ' ')"
fi

# `full` spans VM-capable phases plus a truthful host-Docker phase. OCI image
# building and package smoke require a Docker daemon and are never attributed
# to the daemonless smolvm guest.
if [ "${JOB}" = "full" ]; then
  rc=0
  CI_FULL_PHASE=base    "${CI_SCRIPTS_DIR}/vm-run.sh" --image base    --job full || rc=$?
  [ "${rc}" -eq 0 ] && CI_FULL_PHASE=browser "${CI_SCRIPTS_DIR}/vm-run.sh" --image browser --job full || rc=$?
  [ "${rc}" -eq 0 ] && CI_FULL_PHASE=system  "${CI_SCRIPTS_DIR}/vm-run.sh" --image system  --job full || rc=$?
  [ "${rc}" -eq 0 ] && CI_BACKEND=docker CI_FULL_PHASE=docker "${CI_SCRIPTS_DIR}/vm-run.sh" --image system --job full || rc=$?
  exit "${rc}"
fi

# `standard` = base jobs in the selected backend + OCI validation through the
# explicit host Docker backend.
if [ "${JOB}" = "standard" ]; then
  rc=0
  "${CI_SCRIPTS_DIR}/vm-run.sh" --image base --job standard || rc=$?
  [ "${rc}" -eq 0 ] && CI_BACKEND=docker "${CI_SCRIPTS_DIR}/vm-run.sh" --image system --job image-build || rc=$?
  exit "${rc}"
fi

# Direct Docker-dependent jobs are also routed honestly when the selected
# backend is smolvm.
if [ "${CI_BACKEND:-smolvm}" = "smolvm" ] && { [ "${JOB}" = "image-build" ] || [ "${JOB}" = "package-smoke" ]; }; then
  CI_BACKEND=docker exec "${CI_SCRIPTS_DIR}/vm-run.sh" --image system --job "${JOB}" -- "$@"
fi
exec "${CI_SCRIPTS_DIR}/vm-run.sh" --image "${image}" --job "${JOB}" -- "$@"
