#!/usr/bin/env bash
# scripts/ci/host-run.sh — DIAGNOSTIC execution of a CI job directly on the host.
# Never selected automatically. Prints the diagnostic-boundary warning.
set -euo pipefail

JOB="${1:?usage: host-run.sh <job> [args...]}"
shift || true

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cat >&2 <<'EOF'
WARNING: ci-host is not a hosted-CI or isolated-VM reproduction.

It does not fully validate:
- clean VM state;
- native Linux filesystem;
- systemd;
- privilege boundaries;
- package lifecycle;
- PR merge environment.

Run `make ci` or `make ci-pr` before pushing.
EOF

script="${CI_SCRIPTS_DIR}/${JOB}.sh"
[ -f "${script}" ] || ci_die "unknown CI job '${JOB}' (no ${script})"
rc=0
# Mirror the run-job.sh composites on the host: `full` is a phased set —
# full.sh dies without CI_FULL_PHASE, so run every phase sequentially the
# same way run-job.sh does (#399). `standard` follows with image-build.
if [ "${JOB}" = "full" ]; then
  for phase in base browser system docker; do
    if [ "${rc}" -eq 0 ]; then
      CI_FULL_PHASE="${phase}" bash "${script}" "$@" || rc=$?
    fi
  done
else
  bash "${script}" "$@" || rc=$?
  if [ "${JOB}" = "standard" ] && [ "${rc}" -eq 0 ]; then
    bash "${CI_SCRIPTS_DIR}/image-build.sh" || rc=$?
  fi
fi
exit "${rc}"
