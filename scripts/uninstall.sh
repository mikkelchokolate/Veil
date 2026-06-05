#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${VEIL_INSTALL_DIR:-/usr/local/bin}"
ETC_DIR="${VEIL_ETC_DIR:-/etc/veil}"
VAR_DIR="${VEIL_VAR_DIR:-/var/lib/veil}"
SYSTEMD_DIR="${VEIL_SYSTEMD_DIR:-/etc/systemd/system}"
YES=""
DRY_RUN=""
PURGE=""

usage() {
  cat <<USAGE
Veil uninstaller

Usage:
  curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/uninstall.sh | sudo bash
  curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/uninstall.sh | sudo bash -s -- --yes

Options:
  --install-dir DIR    Directory where veil binary is installed, default /usr/local/bin
  --etc-dir DIR        Veil configuration directory, default /etc/veil
  --var-dir DIR        Veil state directory, default /var/lib/veil
  --systemd-dir DIR    systemd unit directory, default /etc/systemd/system
  --yes                Confirm uninstall without prompt
  --dry-run            Preview what will be removed
  --purge              Also remove configuration and state; preserves the veil system account
  -h, --help           Show this help
USAGE
}

require_value() {
  local flag="$1"
  local value="${2:-}"
  if [[ -z "${value}" || "${value}" == --* ]]; then
    echo "Missing value for ${flag}" >&2
    exit 1
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --install-dir) require_value "$1" "${2:-}"; INSTALL_DIR="$2"; shift 2 ;;
    --etc-dir) require_value "$1" "${2:-}"; ETC_DIR="$2"; shift 2 ;;
    --var-dir) require_value "$1" "${2:-}"; VAR_DIR="$2"; shift 2 ;;
    --systemd-dir) require_value "$1" "${2:-}"; SYSTEMD_DIR="$2"; shift 2 ;;
    --yes) YES="1"; shift ;;
    --dry-run) DRY_RUN="1"; shift ;;
    --purge) PURGE="1"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ "${EUID}" -ne 0 && -z "${DRY_RUN}" ]]; then
  echo "Veil uninstaller must run as root." >&2
  echo "Run with sudo." >&2
  exit 1
fi

VEIL_BIN="${INSTALL_DIR}/veil"
args=(--install-dir "${INSTALL_DIR}" --etc-dir "${ETC_DIR}" --var-dir "${VAR_DIR}" --systemd-dir "${SYSTEMD_DIR}")
if [[ -n "${PURGE}" ]]; then args+=(--purge); fi

if [[ -x "${VEIL_BIN}" ]]; then
  if [[ -n "${DRY_RUN}" ]]; then
    exec "${VEIL_BIN}" uninstall "${args[@]}" --dry-run
  fi
  if [[ -n "${YES}" ]]; then
    exec "${VEIL_BIN}" uninstall "${args[@]}" --yes
  fi
  exec "${VEIL_BIN}" uninstall "${args[@]}"
else
  echo "Veil binary not found at ${VEIL_BIN}" >&2
  echo "Nothing to uninstall." >&2
fi
