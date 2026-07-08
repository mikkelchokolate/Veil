#!/usr/bin/env bash
set -euo pipefail

REPO="${VEIL_REPO:-mikkelchokolate/Veil}"
VERSION="${VEIL_VERSION:-latest}"
INSTALL_DIR="${VEIL_INSTALL_DIR:-/usr/local/bin}"
PROFILE="${VEIL_PROFILE:-ru-recommended}"
DOMAIN=""
EMAIL=""
PANEL_ACCESS=""
PANEL_PORT=""
LE_IP_CERT=""
LE_IP_CERT_PORT=""
YES=""
DRY_RUN=""
FORCE=""
LOCAL_BIN=""

usage() {
  cat <<USAGE
Veil installer

Usage:
  curl -fsSL https://github.com/mikkelchokolate/Veil/releases/latest/download/install.sh | sudo bash
  curl -fsSL https://github.com/mikkelchokolate/Veil/releases/latest/download/install.sh | sudo bash -s -- --profile ru-recommended --panel-access caddy --domain example.com --email admin@example.com

Options:
  Veil install only installs Panel; configure protocols as Panel Inbounds
  --version VERSION    Release tag to install, default latest
  --install-dir DIR    Directory for the veil binary, default /usr/local/bin
  --profile NAME       default or ru-recommended, default ru-recommended
  --domain DOMAIN      Domain used for Panel Caddy access
  --email EMAIL        ACME email for Panel Caddy access
  --panel-access MODE      local, direct, or caddy; prompted interactively when omitted
  --panel-port PORT        Panel TCP port; prompted interactively when omitted; 0 means random high port
  --le-ip-cert             Obtain a Let's Encrypt IP certificate in direct mode (default true)
  --le-ip-cert-port PORT   Port used for Let's Encrypt HTTP-01 validation (default 80)
  --local-bin PATH         Use a local veil binary instead of downloading a release
  --yes                Pass --yes to veil install for non-interactive apply
  --dry-run            Pass --dry-run to veil install
  --force              Force re-install even if veil is already installed
  -h, --help           Show this help

Environment:
  VEIL_REPO            GitHub repo, default mikkelchokolate/Veil
  VEIL_VERSION         Release tag, default latest
  VEIL_INSTALL_DIR     Binary install directory, default /usr/local/bin
  VEIL_LOCAL_BIN       Same as --local-bin
USAGE
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Required command not found: $1" >&2
    exit 1
  fi
}

run_veil_install() {
  if [[ -n "${DRY_RUN}" ]]; then
    "${RUN_BIN}" install "${args[@]}"
    return $?
  fi
  if [[ -z "${YES}" && -r /dev/tty ]]; then
    exec "${RUN_BIN}" install "${args[@]}" < /dev/tty
  fi
  exec "${RUN_BIN}" install "${args[@]}"
}

require_value() {
  local flag="$1"
  local value="${2:-}"
  if [[ -z "${value}" || "${value}" == --* ]]; then
    echo "Missing value for ${flag}" >&2
    exit 1
  fi
}

installed_veil_version() {
  if [[ ! -x "${INSTALL_DIR}/veil" ]]; then
    return 1
  fi
  "${INSTALL_DIR}/veil" version 2>/dev/null | head -n 1 | tr -d '\r'
}

resolve_target_version() {
  if [[ "${VERSION}" != "latest" ]]; then
    printf '%s\n' "${VERSION}"
    return 0
  fi
  local release_json
  release_json="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest")" || return 1
  local tag
  tag="$(printf '%s\n' "${release_json}" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
  if [[ -z "${tag}" ]]; then
    return 1
  fi
  printf '%s\n' "${tag}"
}

version_is_newer() {
  local candidate="${1#v}"
  local current="${2#v}"
  local candidate_major candidate_minor candidate_patch current_major current_minor current_patch
  IFS=. read -r candidate_major candidate_minor candidate_patch _ <<< "${candidate}"
  IFS=. read -r current_major current_minor current_patch _ <<< "${current}"
  candidate_minor="${candidate_minor:-0}"
  candidate_patch="${candidate_patch:-0}"
  current_minor="${current_minor:-0}"
  current_patch="${current_patch:-0}"
  for part in "${candidate_major}" "${candidate_minor}" "${candidate_patch}" "${current_major}" "${current_minor}" "${current_patch}"; do
    if [[ ! "${part}" =~ ^[0-9]+$ ]]; then
      return 1
    fi
  done
  if (( candidate_major > current_major )); then return 0; fi
  if (( candidate_major < current_major )); then return 1; fi
  if (( candidate_minor > current_minor )); then return 0; fi
  if (( candidate_minor < current_minor )); then return 1; fi
  (( candidate_patch > current_patch ))
}

LOCAL_BIN="${VEIL_LOCAL_BIN:-${LOCAL_BIN:-}}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) require_value "$1" "${2:-}"; VERSION="$2"; shift 2 ;;
    --install-dir) require_value "$1" "${2:-}"; INSTALL_DIR="$2"; shift 2 ;;
    --profile) require_value "$1" "${2:-}"; PROFILE="$2"; shift 2 ;;
    --domain) require_value "$1" "${2:-}"; DOMAIN="$2"; shift 2 ;;
    --email) require_value "$1" "${2:-}"; EMAIL="$2"; shift 2 ;;
    --panel-access) require_value "$1" "${2:-}"; PANEL_ACCESS="$2"; shift 2 ;;
    --panel-port) require_value "$1" "${2:-}"; PANEL_PORT="$2"; shift 2 ;;
    --le-ip-cert) LE_IP_CERT="1"; shift ;;
    --le-ip-cert-port) require_value "$1" "${2:-}"; LE_IP_CERT_PORT="$2"; shift 2 ;;
    --local-bin) require_value "$1" "${2:-}"; LOCAL_BIN="$2"; shift 2 ;;
    --yes) YES="1"; shift ;;
    --dry-run) DRY_RUN="1"; shift ;;
    --force) FORCE="1"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

RUN_BIN="${INSTALL_DIR}/veil"

require_cmd curl
require_cmd tar
require_cmd sha256sum
require_cmd uname

if [[ -n "${LOCAL_BIN}" && ! -f "${LOCAL_BIN}" ]]; then
  echo "Local binary not found: ${LOCAL_BIN}" >&2
  exit 1
fi

if [[ "${EUID}" -ne 0 && -z "${DRY_RUN}" ]]; then
  echo "Veil installer must run as root because Panel install writes systemd units and starts services." >&2
  echo "Run with sudo." >&2
  exit 1
fi

# When a local binary is requested, skip version checks and downloads entirely.
if [[ -n "${LOCAL_BIN}" ]]; then
  if [[ -n "${DRY_RUN}" ]]; then
    RUN_BIN="${LOCAL_BIN}"
    echo "Dry run: using local Veil binary ${RUN_BIN} without installing it."
  else
    mkdir -p "${INSTALL_DIR}"
    install -m 0755 "${LOCAL_BIN}" "${INSTALL_DIR}/veil"
    RUN_BIN="${INSTALL_DIR}/veil"
    echo "Installed local binary ${LOCAL_BIN} to ${RUN_BIN}"
  fi
  args=(--profile "${PROFILE}")
  if [[ -n "${PANEL_ACCESS}" ]]; then args+=(--panel-access "${PANEL_ACCESS}"); fi
  if [[ -n "${DOMAIN}" ]]; then args+=(--domain "${DOMAIN}"); fi
  if [[ -n "${EMAIL}" ]]; then args+=(--email "${EMAIL}"); fi
  if [[ -n "${PANEL_PORT}" ]]; then args+=(--panel-port "${PANEL_PORT}"); fi
  if [[ -n "${LE_IP_CERT}" ]]; then args+=(--le-ip-cert); fi
  if [[ -n "${LE_IP_CERT_PORT}" ]]; then args+=(--le-ip-cert-port "${LE_IP_CERT_PORT}"); fi
  if [[ -n "${YES}" ]]; then args+=(--yes); elif [[ -z "${DRY_RUN}" ]]; then args+=(--interactive); fi
  if [[ -n "${DRY_RUN}" ]]; then args+=(--dry-run); fi
  run_veil_install
  exit $?
fi

# Idempotency: skip download only when the installed binary already matches the target.
if [[ -z "${FORCE}" && -f "${INSTALL_DIR}/veil" && -x "${INSTALL_DIR}/veil" ]]; then
  current_version="$(installed_veil_version || true)"
  target_version="$(resolve_target_version || true)"
  if [[ -n "${current_version}" && -n "${target_version}" && "${current_version}" == "${target_version}" ]]; then
    echo "Veil is already installed at ${INSTALL_DIR}/veil"
    echo "Installed Veil ${current_version} is already up to date."
    echo "Use --force to re-install"
    args=(--profile "${PROFILE}")
    if [[ -n "${PANEL_ACCESS}" ]]; then args+=(--panel-access "${PANEL_ACCESS}"); fi
    if [[ -n "${DOMAIN}" ]]; then args+=(--domain "${DOMAIN}"); fi
    if [[ -n "${EMAIL}" ]]; then args+=(--email "${EMAIL}"); fi
    if [[ -n "${PANEL_PORT}" ]]; then args+=(--panel-port "${PANEL_PORT}"); fi
    if [[ -n "${LE_IP_CERT}" ]]; then args+=(--le-ip-cert); fi
    if [[ -n "${LE_IP_CERT_PORT}" ]]; then args+=(--le-ip-cert-port "${LE_IP_CERT_PORT}"); fi
    if [[ -n "${YES}" ]]; then args+=(--yes); elif [[ -z "${DRY_RUN}" ]]; then args+=(--interactive); fi
    if [[ -n "${DRY_RUN}" ]]; then args+=(--dry-run); fi
    run_veil_install
    exit $?
  fi
  if [[ -n "${current_version}" && -n "${target_version}" ]]; then
    if version_is_newer "${target_version}" "${current_version}"; then
      echo "Installed Veil ${current_version} is older than target ${target_version}; upgrading."
    else
      echo "Installed Veil ${current_version} differs from target ${target_version}; installing requested release."
    fi
  else
    echo "Veil is already installed at ${INSTALL_DIR}/veil, but its target version could not be determined; downloading ${VERSION}."
  fi
fi

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "${arch}" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "Unsupported architecture: ${arch}" >&2; exit 1 ;;
esac

case "${os}" in
  linux) ;;
  *) echo "Unsupported OS: ${os}; Veil release installer currently supports Linux." >&2; exit 1 ;;
esac

asset="veil_${os}_${arch}.tar.gz"
base_url="https://github.com/${REPO}/releases"
# URL shape for latest installs: https://github.com/<owner>/<repo>/releases/latest/download/<asset>
if [[ "${VERSION}" == "latest" ]]; then
  download_url="${base_url}/latest/download/${asset}"
  checksums_url="${base_url}/latest/download/checksums.txt"
else
  download_url="${base_url}/download/${VERSION}/${asset}"
  checksums_url="${base_url}/download/${VERSION}/checksums.txt"
fi

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

archive="${tmpdir}/${asset}"
checksums="${tmpdir}/checksums.txt"

echo "Downloading Veil ${VERSION} for ${os}/${arch} from ${REPO}..."
curl -fsSL "${download_url}" -o "${archive}"
curl -fsSL "${checksums_url}" -o "${checksums}"

# Verify checksum with a uniqueness guard so a forged duplicate cannot mask the expected asset.
(
  cd "${tmpdir}"
  count=$(awk -v asset="${asset}" '$2 == asset { count++ } END { print count+0 }' checksums.txt)
  if [[ "${count}" -ne 1 ]]; then
    echo "expected exactly one checksum for ${asset} in checksums.txt, got ${count}" >&2
    exit 1
  fi
  awk -v asset="${asset}" '$2 == asset { print }' checksums.txt | sha256sum -c -
)

tar -xzf "${archive}" -C "${tmpdir}" veil
if [[ ! -f "${tmpdir}/veil" || -L "${tmpdir}/veil" ]]; then
  echo "Downloaded archive did not contain a regular veil binary" >&2
  exit 1
fi
if [[ ! -x "${tmpdir}/veil" ]]; then
  chmod +x "${tmpdir}/veil"
fi

if [[ -n "${DRY_RUN}" ]]; then
  RUN_BIN="${tmpdir}/veil"
  echo "Dry run: using downloaded Veil binary without installing it."
else
  mkdir -p "${INSTALL_DIR}"
  install -m 0755 "${tmpdir}/veil" "${INSTALL_DIR}/veil"
  RUN_BIN="${INSTALL_DIR}/veil"
  echo "Installed ${INSTALL_DIR}/veil"
fi

args=(--profile "${PROFILE}")
if [[ -n "${PANEL_ACCESS}" ]]; then args+=(--panel-access "${PANEL_ACCESS}"); fi
if [[ -n "${DOMAIN}" ]]; then args+=(--domain "${DOMAIN}"); fi
if [[ -n "${EMAIL}" ]]; then args+=(--email "${EMAIL}"); fi
if [[ -n "${PANEL_PORT}" ]]; then args+=(--panel-port "${PANEL_PORT}"); fi
if [[ -n "${LE_IP_CERT}" ]]; then args+=(--le-ip-cert); fi
if [[ -n "${LE_IP_CERT_PORT}" ]]; then args+=(--le-ip-cert-port "${LE_IP_CERT_PORT}"); fi
if [[ -n "${YES}" ]]; then args+=(--yes); elif [[ -z "${DRY_RUN}" ]]; then args+=(--interactive); fi
if [[ -n "${DRY_RUN}" ]]; then args+=(--dry-run); fi

run_veil_install
