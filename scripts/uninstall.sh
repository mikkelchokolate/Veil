#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${VEIL_INSTALL_DIR:-/usr/local/bin}"
YES=""
DRY_RUN=""

usage() {
  cat <<USAGE
Veil uninstaller

Usage:
  curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/uninstall.sh | bash
  curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/uninstall.sh | bash -s -- --yes

Options:
  --install-dir DIR    Directory where veil binary is installed, default /usr/local/bin
  --yes                Confirm uninstall without prompt
  --dry-run            Preview what will be removed
  -h, --help           Show this help
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --install-dir) INSTALL_DIR="$2"; shift 2 ;;
    --yes) YES="1"; shift ;;
    --dry-run) DRY_RUN="1"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ "${EUID}" -ne 0 ]]; then
  echo "Veil uninstaller must run as root." >&2
  echo "Run with sudo." >&2
  exit 1
fi

VEIL_BIN="${INSTALL_DIR}/veil"

if [[ -x "${VEIL_BIN}" ]]; then
  if [[ -n "${DRY_RUN}" ]]; then
    exec "${VEIL_BIN}" uninstall --dry-run
  fi
  if [[ -n "${YES}" ]]; then
    exec "${VEIL_BIN}" uninstall --yes
  fi
  exec "${VEIL_BIN}" uninstall
else
  echo "Veil binary not found at ${VEIL_BIN}" >&2
  echo "Nothing to uninstall." >&2
fi
