#!/usr/bin/env bash
# scripts/ci/vm-run.sh — executes one CI job inside a VM (or VM-equivalent
# isolation for the explicit diagnostic docker backend).
#
# Usage: vm-run.sh --image <base|browser|system> --job <name> [-- <job args>]
#
# Guarantees:
#   - repository enters the guest as a clean git-archive snapshot (never a
#     host bind mount of the working tree);
#   - jobs run on the guest's native Linux filesystem;
#   - artifacts are copied out on success AND failure;
#   - the guest/VM is destroyed afterwards;
#   - the job's exit code is propagated.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

IMAGE_TARGET=""
JOB=""
JOB_ARGS=()
while [ $# -gt 0 ]; do
  case "$1" in
    --image) IMAGE_TARGET="$2"; shift 2 ;;
    --job)   JOB="$2"; shift 2 ;;
    --)      shift; JOB_ARGS=("$@"); break ;;
    *)       ci_die "unknown argument: $1" ;;
  esac
done
if [ -z "${IMAGE_TARGET}" ] || [ -z "${JOB}" ]; then
  ci_die "usage: vm-run.sh --image <base|browser|system> --job <name> [-- args]"
fi

: "${CI_BACKEND:=smolvm}"
: "${CI_CPUS:=4}"
: "${CI_MEMORY:=8}"          # GiB
: "${CI_VM_TIMEOUT:=5400}"   # seconds, per guest run

"${CI_SCRIPTS_DIR}/vm-preflight.sh"

ci_timer_start
if ! "${CI_SCRIPTS_DIR}/vm-build.sh" --check; then
  "${CI_SCRIPTS_DIR}/vm-build.sh"
fi
ci_timer_stop "image-lookup"

KEY="$(cat "${CI_ARTIFACT_DIR}/image-key.txt")"
IMAGE_TAG="veil-ci-${IMAGE_TARGET}:${KEY}"

# --- Snapshot -----------------------------------------------------------------
ci_timer_start
EXCHANGE="$(mktemp -d "${CI_ARTIFACT_DIR}/vm-exchange.XXXXXX")"
mkdir -p "${EXCHANGE}/artifacts"
SNAPSHOT="${EXCHANGE}/snapshot.tar"
"${CI_SCRIPTS_DIR}/snapshot.sh" "${CI_TREEISH:-HEAD}" "${SNAPSHOT}"
# The guest workspace is a fresh `git init` repo, so in-guest `git rev-parse
# HEAD` does NOT identify the source commit. Pass the real SHA through.
CI_SOURCE_SHA="$(git -C "${CI_ROOT}" rev-parse "${CI_TREEISH:-HEAD}^{commit}" 2>/dev/null || echo unknown)"
export CI_SOURCE_SHA
ci_timer_stop "snapshot-create"

ACTIVE_SMOLVM_MACHINE=""
ROOTFS_EXPORT_CONTAINER=""
ROOTFS_EXPORT_TMP=""

# shellcheck disable=SC2317  # invoked indirectly via trap
cleanup() {
  rc=$?
  if [ -n "${ACTIVE_SMOLVM_MACHINE}" ]; then
    smolvm machine stop --name "${ACTIVE_SMOLVM_MACHINE}" >/dev/null 2>&1 || true
    smolvm machine delete --name "${ACTIVE_SMOLVM_MACHINE}" --force >/dev/null 2>&1 || true
  fi
  [ -z "${ROOTFS_EXPORT_CONTAINER}" ] || docker rm -f "${ROOTFS_EXPORT_CONTAINER}" >/dev/null 2>&1 || true
  [ -z "${ROOTFS_EXPORT_TMP}" ] || rm -rf "${ROOTFS_EXPORT_TMP}"
  # Merge guest artifacts into the run's artifact dir (success and failure).
  if [ -d "${EXCHANGE}/artifacts" ]; then
    cp -rf "${EXCHANGE}/artifacts/." "${CI_ARTIFACT_DIR}/" 2>/dev/null || true
  fi
  rm -rf "${EXCHANGE}"
  if [ "${CACHE_EPHEMERAL:-0}" = "1" ] && [ -n "${CACHE_ROOT:-}" ]; then
    rm -rf "${CACHE_ROOT}"
  fi
  exit "${rc}"
}
trap cleanup EXIT

# Dependency caches may persist between ordinary runs, but CI_CLEAN=1 must
# exercise a genuinely cold dependency path. Neither mode persists test state.
CACHE_EPHEMERAL=0
if [ "${CI_CLEAN:-0}" = "1" ]; then
  CACHE_ROOT="$(mktemp -d /tmp/veil-ci-cold-cache.XXXXXX)"
  CACHE_EPHEMERAL=1
  ci_log "CI_CLEAN=1: using ephemeral dependency caches at ${CACHE_ROOT}"
else
  CACHE_ROOT="${HOME}/.cache/veil-ci"
fi
mkdir -p "${CACHE_ROOT}/gomod" "${CACHE_ROOT}/gobuild" "${CACHE_ROOT}/pnpm" "${CACHE_ROOT}/playwright"
if [ "$(id -u)" -eq 0 ]; then
  chown -R 1000:1000 "${CACHE_ROOT}" 2>/dev/null || true
fi

smolvm_stop_active() {
  [ -n "${ACTIVE_SMOLVM_MACHINE}" ] || return 0
  smolvm machine stop --name "${ACTIVE_SMOLVM_MACHINE}" >/dev/null 2>&1 || true
  smolvm machine delete --name "${ACTIVE_SMOLVM_MACHINE}" --force >/dev/null 2>&1 || true
  ACTIVE_SMOLVM_MACHINE=""
}

run_job_smolvm() {
  # smolvm can consume a pre-expanded rootfs directly. This avoids its
  # guest-side crane extraction path for docker-save archives, which is not
  # supported by every KVM host filesystem. Keep the expansion content-keyed
  # and atomic on the same HDD-backed cache as dependency stores.
  local image_rootfs="${CACHE_ROOT}/rootfs/veil-ci-${IMAGE_TARGET}-${KEY}"
  if [ ! -f "${image_rootfs}/.veil-ci-rootfs-complete" ]; then
    ci_step "expand ${IMAGE_TAG} for smolvm"
    ROOTFS_EXPORT_TMP="${image_rootfs}.tmp.$$"
    rm -rf "${ROOTFS_EXPORT_TMP}"
    mkdir -p "${ROOTFS_EXPORT_TMP}" "$(dirname "${image_rootfs}")"
    if ! ROOTFS_EXPORT_CONTAINER="$(docker create "${IMAGE_TAG}")"; then return 1; fi
    if ! docker export "${ROOTFS_EXPORT_CONTAINER}" | tar -xpf - -C "${ROOTFS_EXPORT_TMP}"; then return 1; fi
    if ! docker rm "${ROOTFS_EXPORT_CONTAINER}" >/dev/null; then return 1; fi
    ROOTFS_EXPORT_CONTAINER=""
    if ! { printf '%s\n' "${KEY}" > "${ROOTFS_EXPORT_TMP}/.veil-ci-rootfs-complete" && \
      rm -rf "${image_rootfs}" && mv "${ROOTFS_EXPORT_TMP}" "${image_rootfs}"; }; then return 1; fi
    ROOTFS_EXPORT_TMP=""
  fi
  ci_step "smolvm run ${IMAGE_TARGET} job=${JOB}"
  if [ "${IMAGE_TARGET}" != "system" ]; then
    timeout "${CI_VM_TIMEOUT}" smolvm machine run \
      --image "${image_rootfs}" \
      --name "veil-ci-${IMAGE_TARGET}-$$" \
      --cpus "${CI_CPUS}" --mem "$(( CI_MEMORY * 1024 ))" \
      --net \
      --volume "${EXCHANGE}:/exchange" \
      --volume "${CACHE_ROOT}/gomod:/home/ci/go/pkg/mod" \
      --volume "${CACHE_ROOT}/gobuild:/home/ci/.cache/go-build" \
      --volume "${CACHE_ROOT}/pnpm:/home/ci/.local/share/pnpm/store" \
      -- /opt/ci/guest-run.sh "${JOB}" "${JOB_ARGS[@]:-}"
    return
  fi

  local machine="veil-ci-system-$$"
  local rc=0
  printf '%s\n' "${JOB}" > "${EXCHANGE}/job"
  printf '%s\n' "${CI_FULL_PHASE:-system}" > "${EXCHANGE}/full-phase"
  printf '%s\n' "${CI_SOURCE_SHA}" > "${EXCHANGE}/source-sha"
  : > "${EXCHANGE}/systemd-run-request"
  ACTIVE_SMOLVM_MACHINE="${machine}"
  smolvm machine create --name "${machine}" --image "${image_rootfs}" \
    --cpus "${CI_CPUS}" --mem "$(( CI_MEMORY * 1024 ))" --net \
    --volume "${EXCHANGE}:/exchange" \
    --volume "${CACHE_ROOT}/gomod:/home/ci/go/pkg/mod" \
    --volume "${CACHE_ROOT}/gobuild:/home/ci/.cache/go-build" \
    --volume "${CACHE_ROOT}/pnpm:/home/ci/.local/share/pnpm/store" \
    -- /sbin/init || return 1
  smolvm machine start --name "${machine}" || return 1
  local deadline=$(( SECONDS + CI_VM_TIMEOUT ))
  while [ ! -f "${EXCHANGE}/result" ] && [ "${SECONDS}" -lt "${deadline}" ]; do
    sleep 1
  done
  if [ ! -f "${EXCHANGE}/result" ]; then
    ci_warn "systemd workload did not publish a result within ${CI_VM_TIMEOUT}s"
    rc=1
  else
    rc="$(tr -d '\r\n' < "${EXCHANGE}/result")"
    case "${rc}" in (''|*[!0-9]*) rc=1 ;; esac
  fi
  smolvm_stop_active
  return "${rc}"
}

run_job_docker_simple() { # base/browser: one ephemeral container per job
  local extra=("$@")
  ci_step "docker run ${IMAGE_TARGET} job=${JOB}"
  timeout "${CI_VM_TIMEOUT}" docker run --rm \
    --name "veil-ci-${IMAGE_TARGET}-$$" \
    --cpus "${CI_CPUS}" --memory "${CI_MEMORY}g" \
    --shm-size 1g \
    --network host \
    -v "${EXCHANGE}:/exchange" \
    -v "${CACHE_ROOT}/gomod:/home/ci/go/pkg/mod" \
    -v "${CACHE_ROOT}/gobuild:/home/ci/.cache/go-build" \
    -v "${CACHE_ROOT}/pnpm:/home/ci/.local/share/pnpm/store" \
    -v "${CACHE_ROOT}/playwright:/home/ci/.cache/ms-playwright" \
    -e CI_FULL_PHASE="${CI_FULL_PHASE:-}" \
    -e CI_SOURCE_SHA="${CI_SOURCE_SHA}" \
    "${extra[@]}" \
    "${IMAGE_TAG}" "${JOB}" "${JOB_ARGS[@]:-}"
}

run_job_docker_systemd() {
  # system image: boot systemd as PID 1, then exec the job inside.
  # The workspace lives under the exchange dir at the SAME absolute path on
  # host and guest so package-smoke's `docker run -v $repo/...` resolves on
  # the host daemon (Docker-out-of-Docker path alignment).
  local ctr="veil-ci-system-$$"
  ci_step "docker systemd VM ${IMAGE_TARGET} job=${JOB}"
  docker rm -f "${ctr}" >/dev/null 2>&1 || true
  docker run -d \
    --name "${ctr}" \
    --privileged \
    --cgroupns=host \
    --cpus "${CI_CPUS}" --memory "${CI_MEMORY}g" \
    -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
    --network host \
    -v /var/run/docker.sock:/opt/oci/docker.sock \
    -v "${EXCHANGE}:/exchange" \
    -v "${EXCHANGE}:${EXCHANGE}" \
    -e CI_WORKSPACE_OVERRIDE="${EXCHANGE}/veil" \
    -v "${CACHE_ROOT}/gomod:/home/ci/go/pkg/mod" \
    -v "${CACHE_ROOT}/gobuild:/home/ci/.cache/go-build" \
    -v "${CACHE_ROOT}/pnpm:/home/ci/.local/share/pnpm/store" \
    -v "${CACHE_ROOT}/gomod:/root/go/pkg/mod" \
    -v "${CACHE_ROOT}/gobuild:/root/.cache/go-build" \
    --entrypoint /lib/systemd/systemd \
    "${IMAGE_TAG}" >/dev/null

  local booted=1
  for _ in $(seq 1 60); do
    if docker exec "${ctr}" systemctl is-system-running 2>/dev/null | grep -Eq 'running|degraded'; then
      booted=0; break
    fi
    sleep 1
  done
  if [ "${booted}" -ne 0 ]; then
    docker logs "${ctr}" >&2 || true
    docker rm -f "${ctr}" >/dev/null 2>&1 || true
    ci_die "systemd did not reach running state in the system VM"
  fi
  ci_log "systemd VM booted (pid1=$(docker exec "${ctr}" cat /proc/1/comm))"

  # Prove that this is a real booted systemd environment before running product
  # jobs: PID 1, D-Bus, service lifecycle, socket activation, and journal.
  local poc_rc=0
  timeout 120 docker exec "${ctr}" /opt/ci/systemd/poc.sh || poc_rc=$?
  if [ "${poc_rc}" -ne 0 ]; then
    docker logs "${ctr}" >&2 || true
    docker rm -f "${ctr}" >/dev/null 2>&1 || true
    ci_die "systemd lifecycle proof failed in the system VM (rc=${poc_rc})"
  fi

  local rc=0
  timeout "${CI_VM_TIMEOUT}" docker exec \
    -e CI_FULL_PHASE="${CI_FULL_PHASE:-system}" \
    -e DOCKER_HOST="unix:///opt/oci/docker.sock" \
    -e CI_SOURCE_SHA="${CI_SOURCE_SHA}" \
    "${ctr}" /opt/ci/guest-run.sh "${JOB}" "${JOB_ARGS[@]:-}" || rc=$?

  # Journal for the record, then destroy the VM.
  docker exec "${ctr}" sh -c 'journalctl --no-pager -n 400 > /exchange/artifacts/systemd-journal.txt 2>&1' || true
  docker rm -f "${ctr}" >/dev/null 2>&1 || true
  return "${rc}"
}

rc=0
case "${CI_BACKEND}" in
  smolvm)
    run_job_smolvm || rc=$?
    ;;
  docker)
    case "${IMAGE_TARGET}" in
      system) run_job_docker_systemd || rc=$? ;;
      browser) run_job_docker_simple --cap-add SYS_ADMIN || rc=$? ;;
      *)       run_job_docker_simple || rc=$? ;;
    esac
    ;;
esac
ci_timer_stop "vm-job:${JOB}"

if [ "${rc}" -ne 0 ]; then
  ci_fail_banner
fi
exit "${rc}"
