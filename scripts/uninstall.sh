#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${VEIL_INSTALL_DIR:-/usr/local/bin}"
ETC_DIR="${VEIL_ETC_DIR:-/etc/veil}"
VAR_DIR="${VEIL_VAR_DIR:-/var/lib/veil}"
SYSTEMD_DIR="${VEIL_SYSTEMD_DIR:-/etc/systemd/system}"
# Packaged (vendor) unit directories the nfpm packages install into — distinct
# from SYSTEMD_DIR, which holds units written by `veil install` (#492).
VENDOR_SYSTEMD_DIRS="${VEIL_VENDOR_SYSTEMD_DIRS:-/lib/systemd/system /usr/lib/systemd/system}"
SYSCTL_CONF="${VEIL_SYSCTL_CONF:-/etc/sysctl.d/99-veil-quic.conf}"
CADDY_STATE_DIR="${VEIL_CADDY_STATE_DIR:-/var/lib/caddy}"
MITA_STATE_DIR="${VEIL_MITA_STATE_DIR:-/var/lib/mita}"
YES=""
DRY_RUN=""
PURGE=""
KEEP_DATA=""

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
  --keep-data          Preserve configuration and state so a reinstall reuses the existing credentials and panel path
  --purge              Remove configuration and state (now the default; kept for compatibility)
  -h, --help           Show this help

By default uninstall removes configuration and state in /etc/veil and /var/lib/veil
so a later install starts fresh with a new password. The veil system account is preserved.
Units written by `veil install` (SYSTEMD_DIR) and packaged units under
/lib/systemd/system + /usr/lib/systemd/system (VEIL_VENDOR_SYSTEMD_DIRS) are
always removed, as is the QUIC sysctl drop-in (VEIL_SYSCTL_CONF).
Purge also removes the Caddy/Mita state dirs (/var/lib/caddy, /var/lib/mita);
if those are shared with a system Caddy/mita outside Veil, use --keep-data or
override VEIL_CADDY_STATE_DIR / VEIL_MITA_STATE_DIR.
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
    --keep-data) KEEP_DATA="1"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

require_root() {
  if [[ "${EUID}" -ne 0 && -z "${DRY_RUN}" ]]; then
    echo "Veil uninstaller must run as root." >&2
    echo "Run with sudo." >&2
    exit 1
  fi
}

VEIL_BIN="${INSTALL_DIR}/veil"
args=(--install-dir "${INSTALL_DIR}" --etc-dir "${ETC_DIR}" --var-dir "${VAR_DIR}" --systemd-dir "${SYSTEMD_DIR}")
if [[ -n "${PURGE}" ]]; then args+=(--purge); fi
if [[ -n "${KEEP_DATA}" ]]; then args+=(--keep-data); fi

has_leftover_state() {
  [[ -e "${ETC_DIR}" || -e "${VAR_DIR}" ]] && return 0
  [[ -e "${CADDY_STATE_DIR}" || -e "${MITA_STATE_DIR}" ]] && return 0
  [[ -e "${SYSCTL_CONF}" ]] && return 0
  local dir unit
  # shellcheck disable=SC2086 # VENDOR_SYSTEMD_DIRS is a space-separated list.
  for dir in "${SYSTEMD_DIR}" ${VENDOR_SYSTEMD_DIRS}; do
    [[ -d "${dir}/veil-backup.service.d" ]] && return 0
    for unit in "${dir}"/veil*.service "${dir}"/veil*.socket "${dir}"/veil*.timer \
      "${dir}"/multi-user.target.wants/veil-*@*.service; do
      if [[ -e "${unit}" || -L "${unit}" ]]; then
        return 0
      fi
    done
  done
  return 1
}

print_leftover_plan() {
  echo "Veil binary not found at ${VEIL_BIN}; leftover host state remains:"
  if [[ -z "${KEEP_DATA}" ]]; then
    echo "  - ${ETC_DIR}"
    echo "  - ${VAR_DIR}"
    echo "  - ${CADDY_STATE_DIR}"
    echo "  - ${MITA_STATE_DIR}"
  else
    echo "  - keep ${ETC_DIR}"
    echo "  - keep ${VAR_DIR}"
    echo "  - keep ${CADDY_STATE_DIR}"
    echo "  - keep ${MITA_STATE_DIR}"
  fi
  local dir
  # shellcheck disable=SC2086 # VENDOR_SYSTEMD_DIRS is a space-separated list.
  for dir in "${SYSTEMD_DIR}" ${VENDOR_SYSTEMD_DIRS}; do
    echo "  - ${dir}/veil*.service"
    echo "  - ${dir}/veil*.socket"
    echo "  - ${dir}/veil*.timer"
    echo "  - ${dir}/veil-backup.service.d"
    echo "  - ${dir}/multi-user.target.wants/veil-*@*.service"
  done
  echo "  - ${SYSCTL_CONF}"
}

remove_leftover_state() {
  if command -v systemctl >/dev/null 2>&1; then
    systemctl stop veil.service veil-helper.service veil-helper.socket veil-caddy.service veil-mieru.service veil-warp.service veil-backup.service veil-backup.timer >/dev/null 2>&1 || true
    systemctl disable veil.service veil-helper.service veil-helper.socket veil-caddy.service veil-mieru.service veil-warp.service veil-backup.service veil-backup.timer >/dev/null 2>&1 || true
    systemctl stop 'veil-hysteria2@*' 'veil-olcrtc@*' >/dev/null 2>&1 || true
    # Template-glob disable cannot clear per-instance wants links (audit #176);
    # stop/disable each concrete instance found under multi-user.target.wants.
    local instance
    for instance in "${SYSTEMD_DIR}"/multi-user.target.wants/veil-*@*.service; do
      [[ -e "${instance}" || -L "${instance}" ]] || continue
      systemctl stop "$(basename "${instance}")" >/dev/null 2>&1 || true
      systemctl disable "$(basename "${instance}")" >/dev/null 2>&1 || true
    done
  fi
  if [[ -z "${KEEP_DATA}" ]]; then
    rm -rf "${ETC_DIR}" "${VAR_DIR}" "${CADDY_STATE_DIR}" "${MITA_STATE_DIR}"
  fi
  local dir
  # shellcheck disable=SC2086 # VENDOR_SYSTEMD_DIRS is a space-separated list.
  for dir in "${SYSTEMD_DIR}" ${VENDOR_SYSTEMD_DIRS}; do
    rm -rf "${dir}/veil-backup.service.d"
    rm -f "${dir}"/veil*.service "${dir}"/veil*.socket "${dir}"/veil*.timer
    rm -f "${dir}"/multi-user.target.wants/veil-*@*.service
  done
  if [[ -e "${SYSCTL_CONF}" ]]; then
    rm -f "${SYSCTL_CONF}"
    echo "Removed ${SYSCTL_CONF}; live kernel values reset at next boot (or via 'sysctl --system')."
  fi
  if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
  fi
}

if [[ -x "${VEIL_BIN}" ]]; then
  require_root
  if [[ -n "${DRY_RUN}" ]]; then
    exec "${VEIL_BIN}" uninstall "${args[@]}" --dry-run
  fi
  if [[ -n "${YES}" ]]; then
    exec "${VEIL_BIN}" uninstall "${args[@]}" --yes
  fi
  exec "${VEIL_BIN}" uninstall "${args[@]}"
fi

if ! has_leftover_state; then
  echo "Veil binary not found at ${VEIL_BIN}" >&2
  echo "Nothing to uninstall." >&2
  exit 0
fi

print_leftover_plan
if [[ -n "${DRY_RUN}" ]]; then
  exit 0
fi
if [[ -z "${YES}" ]]; then
  echo "Re-run with --yes to remove leftover configuration, state, and units." >&2
  exit 1
fi
require_root
remove_leftover_state
echo "Uninstalled leftover Veil host state."
