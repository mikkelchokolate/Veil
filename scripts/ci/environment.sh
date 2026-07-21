#!/usr/bin/env bash
# scripts/ci/environment.sh — writes the environment manifest for a CI job.
# Same content locally (VM) and in GitHub Actions; secrets are redacted.
set -euo pipefail

JOB="${1:-unknown}"

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

manifest="${CI_ARTIFACT_DIR}/environment-${JOB}.txt"

{
  echo "# CI environment manifest — job: ${JOB}"
  echo "date: $(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  echo
  echo "## uname";        uname -a || true
  echo; echo "## os-release"; cat /etc/os-release || true
  echo; echo "## id";         id || true
  echo; echo "## umask";      umask || true
  echo; echo "## go";         go version 2>/dev/null || echo "go: not installed"
  go env 2>/dev/null || true
  echo; echo "## node";       node --version 2>/dev/null || echo "node: not installed"
  echo; echo "## pnpm";       pnpm --version 2>/dev/null || echo "pnpm: not installed"
  echo; echo "## git";        git --version || true
  echo; echo "## systemctl";  systemctl --version 2>/dev/null | head -2 || echo "systemctl: not installed"
  echo; echo "## locale";     locale 2>/dev/null || true
  echo; echo "## timezone";   (timedatectl 2>/dev/null || cat /etc/timezone 2>/dev/null || echo "${TZ}") || true
  echo; echo "## pid1";       cat /proc/1/comm || true
  echo; echo "## mount";      mount | grep -Ev 'cgroup|proc|sysfs|devpts|mqueue|shm' || true
  echo; echo "## df";         df -h / /workspace 2>/dev/null || df -h / || true
  if [ "${JOB}" = "privilege-boundary" ] || [ "${JOB}" = "e2e" ] || [ "${JOB}" = "package-smoke" ] || [ "${JOB}" = "image-build" ]; then
    echo; echo "## systemd state"; systemctl is-system-running 2>&1 || true
    echo; echo "## docker";        docker version 2>&1 | head -8 || echo "docker: not available"
    echo; echo "## buildctl";      buildctl --version 2>&1 || echo "buildctl: not available"
  fi
  echo; echo "## environment (redacted)"
  env | sort | sed -E 's/^([^=]*(TOKEN|SECRET|PASSWORD|AUTH|COOKIE|KEY|CREDENTIAL|PRIVATE|SESSION)[^=]*)=.*/\1=<redacted>/I'
} > "${manifest}" 2>&1

ci_log "environment manifest: ${manifest}"
