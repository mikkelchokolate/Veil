#!/usr/bin/env bash
set -euo pipefail

REPO="${VEIL_REPO:-mikkelchokolate/Veil}"
VERSION="${VEIL_VERSION:-latest}"
INSTALL_DIR="${VEIL_INSTALL_DIR:-/usr/local/bin}"
PROFILE="${VEIL_PROFILE:-ru-recommended}"
DOMAIN=""
EMAIL=""
STACK="panel"
PANEL_ACCESS="local"
PANEL_PORT=""
YES=""
DRY_RUN=""
FORCE=""

usage() {
  cat <<USAGE
Veil installer

Usage:
  curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | sudo bash
  curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | sudo bash -s -- --profile ru-recommended --panel-access caddy --domain example.com --email admin@example.com

Options:
  Install is Panel-only; configure protocols from the Panel after login.
  --version VERSION    Release tag to install, default latest
  --install-dir DIR    Directory for the veil binary, default /usr/local/bin
  --profile NAME       default or ru-recommended, default ru-recommended
  --domain DOMAIN      Domain used for Panel Caddy access
  --email EMAIL        ACME email for Panel Caddy access
  --panel-access MODE  local, direct, or caddy, default local
  --panel-port PORT    Panel TCP port, default 2096; 0 means random high port in veil install
  --yes                Pass --yes to veil install for non-interactive apply
  --dry-run            Pass --dry-run to veil install
  --force              Force re-install even if veil is already installed
  -h, --help           Show this help

Environment:
  VEIL_REPO            GitHub repo, default mikkelchokolate/Veil
  VEIL_VERSION         Release tag, default latest
  VEIL_INSTALL_DIR     Binary install directory, default /usr/local/bin
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

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) require_value "$1" "${2:-}"; VERSION="$2"; shift 2 ;;
    --install-dir) require_value "$1" "${2:-}"; INSTALL_DIR="$2"; shift 2 ;;
    --profile) require_value "$1" "${2:-}"; PROFILE="$2"; shift 2 ;;
    --domain) require_value "$1" "${2:-}"; DOMAIN="$2"; shift 2 ;;
    --email) require_value "$1" "${2:-}"; EMAIL="$2"; shift 2 ;;
    --port) require_value "$1" "${2:-}"; shift 2 ;;
    --stack) require_value "$1" "${2:-}"; STACK="$2"; shift 2 ;;
    --panel-access) require_value "$1" "${2:-}"; PANEL_ACCESS="$2"; shift 2 ;;
    --panel-port) require_value "$1" "${2:-}"; PANEL_PORT="$2"; shift 2 ;;
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

if [[ "${EUID}" -ne 0 && -z "${DRY_RUN}" ]]; then
  echo "Veil installer must run as root because Panel install writes systemd units and starts services." >&2
  echo "Run with sudo." >&2
  exit 1
fi

if [[ "${STACK}" != "panel" ]]; then
  echo "Veil install only installs Panel; configure protocols as Panel Inbounds." >&2
  exit 1
fi

# Idempotency: skip download if veil is already installed and --force not set
if [[ -z "${FORCE}" && -f "${INSTALL_DIR}/veil" && -x "${INSTALL_DIR}/veil" ]]; then
  echo "Veil is already installed at ${INSTALL_DIR}/veil"
  echo "Use --force to re-install"
  args=(--profile "${PROFILE}" --panel-access "${PANEL_ACCESS}")
  if [[ -n "${DOMAIN}" ]]; then args+=(--domain "${DOMAIN}"); fi
  if [[ -n "${EMAIL}" ]]; then args+=(--email "${EMAIL}"); fi
  if [[ -n "${PANEL_PORT}" ]]; then args+=(--panel-port "${PANEL_PORT}"); fi
  if [[ -n "${YES}" ]]; then args+=(--yes); elif [[ -z "${DRY_RUN}" ]]; then args+=(--interactive); fi
  if [[ -n "${DRY_RUN}" ]]; then args+=(--dry-run); fi
  run_veil_install
  exit $?
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

# Verify checksum with match-count guard: exit if grep finds no matching line
(
  cd "${tmpdir}"
  count=$(grep -c "  ${asset}$" checksums.txt)
  if [[ "${count}" -eq 0 ]]; then
    echo "no matching checksum for ${asset} in checksums.txt" >&2
    exit 1
  fi
  grep "  ${asset}$" checksums.txt | sha256sum -c -
)

tar -xzf "${archive}" -C "${tmpdir}"
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

args=(--profile "${PROFILE}" --panel-access "${PANEL_ACCESS}")
if [[ -n "${DOMAIN}" ]]; then args+=(--domain "${DOMAIN}"); fi
if [[ -n "${EMAIL}" ]]; then args+=(--email "${EMAIL}"); fi
if [[ -n "${PANEL_PORT}" ]]; then args+=(--panel-port "${PANEL_PORT}"); fi
if [[ -n "${YES}" ]]; then args+=(--yes); elif [[ -z "${DRY_RUN}" ]]; then args+=(--interactive); fi
if [[ -n "${DRY_RUN}" ]]; then args+=(--dry-run); fi

run_veil_install
