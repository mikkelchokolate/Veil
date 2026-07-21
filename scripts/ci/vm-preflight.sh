#!/usr/bin/env bash
# scripts/ci/vm-preflight.sh — validates the selected CI backend before any work.
# CI_BACKEND=smolvm (default, authoritative) requires smolvm + hardware
# virtualization. CI_BACKEND=docker is an explicit, opt-in diagnostic backend
# (never selected automatically, always prints a warning).
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

: "${CI_BACKEND:=smolvm}"

case "${CI_BACKEND}" in
  smolvm)
    command -v smolvm >/dev/null 2>&1 || ci_die "\
Local CI requires smolvm and hardware virtualization.

smolvm is not installed. Install a version >= ${CI_SMOLVM_MIN_VERSION}:
  https://github.com/smol-machines/smolvm#install

No fallback was performed.

See:
docs/development/ci.md#virtualization-setup"
    if [ ! -e /dev/kvm ] && [ "$(uname -s)" = "Linux" ]; then
      smolvm_out="$(smolvm machine run --image alpine -- true 2>&1 || true)"
      ci_die "\
Local CI requires smolvm and hardware virtualization.

/dev/kvm is unavailable on this host (no nested virtualization).
smolvm probe said:
  $(echo "${smolvm_out}" | tail -1)

No fallback was performed.

See:
docs/development/ci.md#virtualization-setup"
    fi
    ;;
  docker)
    ci_warn "\
CI_BACKEND=docker is an explicit diagnostic backend.

It is NOT an authoritative CI reproduction: no hardware isolation, host kernel,
no smolvm. Use the default CI_BACKEND=smolvm on a virtualization-capable host."
    command -v docker >/dev/null 2>&1 || ci_die "CI_BACKEND=docker but docker is not installed"
    docker info >/dev/null 2>&1 || ci_die "CI_BACKEND=docker but the docker daemon is unreachable"
    ;;
  *)
    ci_die "unknown CI_BACKEND '${CI_BACKEND}' (valid: smolvm, docker)"
    ;;
esac

# Resource preflight (host).
mem_kb="$(awk '/MemTotal/ {print $2}' /proc/meminfo)"
mem_gb=$(( mem_kb / 1024 / 1024 ))
: "${CI_MEMORY:=8}"
req=$(( CI_MEMORY + 2 ))
if [ "${mem_gb}" -lt "${req}" ]; then
  ci_warn "host has ${mem_gb} GiB RAM; CI_MEMORY=${CI_MEMORY} GiB VM plus host needs ~${req} GiB"
fi
ci_log "backend=${CI_BACKEND} preflight ok (cpus=$(nproc), mem=${mem_gb}GiB)"
