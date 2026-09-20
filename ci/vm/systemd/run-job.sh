#!/usr/bin/env bash
# Runs as a systemd service inside the smolvm system-image container. The host
# stages request files before execing /sbin/init; this service proves PID 1,
# D-Bus and socket activation, runs the requested job, records its status, then
# asks systemd to shut the container down cleanly.
set -uo pipefail

exchange=/exchange
result="${exchange}/result"
mkdir -p "${exchange}/artifacts"
rc=0

job="$(tr -d '\r\n' < "${exchange}/job" 2>/dev/null || true)"
phase="$(tr -d '\r\n' < "${exchange}/full-phase" 2>/dev/null || true)"
source_sha="$(tr -d '\r\n' < "${exchange}/source-sha" 2>/dev/null || true)"
# vm-run.sh stages job arguments one-per-line in job-args (spaces preserved);
# forward them to guest-run unchanged — previously only the job name reached
# the guest, so args silently vanished on the system-smolvm path (#474).
job_args=()
if [ -f "${exchange}/job-args" ]; then
  while IFS= read -r arg; do
    job_args+=("${arg}")
  done < "${exchange}/job-args"
fi
case "${job}" in privilege-boundary|e2e|full|install-acceptance) ;; *) echo "invalid system job: ${job}" >&2; rc=2 ;; esac

if [ "${rc}" -eq 0 ]; then
  for _ in $(seq 1 60); do
    state="$(systemctl is-system-running 2>/dev/null || true)"
    [[ "${state}" =~ ^(running|degraded)$ ]] && break
    sleep 1
  done
  [[ "${state:-}" =~ ^(running|degraded)$ ]] || { echo "systemd readiness failed: ${state:-unknown}" >&2; rc=1; }
fi
if [ "${rc}" -eq 0 ]; then
  /opt/ci/systemd/poc.sh || rc=$?
fi
if [ "${rc}" -eq 0 ]; then
  CI_FULL_PHASE="${phase}" CI_SOURCE_SHA="${source_sha}" /opt/ci/guest-run.sh "${job}" "${job_args[@]}" || rc=$?
fi
journalctl --no-pager -n 1000 > "${exchange}/artifacts/systemd-journal.txt" 2>&1 || true
printf '%s\n' "${rc}" > "${result}"
sync
systemctl --no-block poweroff || true
exit 0
