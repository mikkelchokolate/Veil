#!/usr/bin/env bash
# scripts/ci/run-job.sh — maps a logical CI job to its image and executes it in
# a VM. Used by `make ci`, `make ci-full`, `make ci-pr`, `make ci-job`.
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

# `full` spans all three images: run the phases in their own guests.
if [ "${JOB}" = "full" ]; then
  rc=0
  CI_FULL_PHASE=base    "${CI_SCRIPTS_DIR}/vm-run.sh" --image base    --job full || rc=$?
  [ "${rc}" -eq 0 ] && CI_FULL_PHASE=browser "${CI_SCRIPTS_DIR}/vm-run.sh" --image browser --job full || rc=$?
  [ "${rc}" -eq 0 ] && CI_FULL_PHASE=system  "${CI_SCRIPTS_DIR}/vm-run.sh" --image system  --job full || rc=$?
  exit "${rc}"
fi

# `standard` = base jobs (frontend/test/lint) + OCI build validation in the
# system image (the only job that needs the OCI daemon socket).
if [ "${JOB}" = "standard" ]; then
  rc=0
  "${CI_SCRIPTS_DIR}/vm-run.sh" --image base --job standard || rc=$?
  [ "${rc}" -eq 0 ] && "${CI_SCRIPTS_DIR}/vm-run.sh" --image system --job image-build || rc=$?
  exit "${rc}"
fi
exec "${CI_SCRIPTS_DIR}/vm-run.sh" --image "${image}" --job "${JOB}" -- "$@"
