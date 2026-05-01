#!/usr/bin/env bash
set -euo pipefail

REPO="${VEIL_REPO:-mikkelchokolate/Veil}"
VERSION="${VEIL_VERSION:-latest}"
INSTALL_DIR="${VEIL_INSTALL_DIR:-/usr/local/bin}"
PROFILE="${VEIL_PROFILE:-ru-recommended}"
DOMAIN=""
EMAIL=""
PORT=""
STACK="both"
PANEL_PORT=""
YES=""
DRY_RUN=""

usage() {
  cat <<USAGE
Veil installer

Usage:
  curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | bash
  curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | bash -s -- --profile ru-recommended --domain example.com --email admin@example.com --port 443

Options:
  --version VERSION    Release tag to install, default latest
  --install-dir DIR    Directory for the veil binary, default /usr/local/bin
  --profile NAME       default or ru-recommended, default ru-recommended
  --domain DOMAIN      Domain used for ACME and client configs
  --email EMAIL        ACME email
  --port PORT          Shared proxy port passed to veil install; omit it to use the interactive prompt
  --stack STACK        naive, hysteria2, or both, default both
  --panel-port PORT    Panel TCP port; 0 means random high port in veil install
  --yes                Pass --yes to veil install for non-interactive apply
  --dry-run            Pass --dry-run to veil install
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

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --install-dir) INSTALL_DIR="$2"; shift 2 ;;
    --profile) PROFILE="$2"; shift 2 ;;
    --domain) DOMAIN="$2"; shift 2 ;;
    --email) EMAIL="$2"; shift 2 ;;
    --port) PORT="$2"; shift 2 ;;
    --stack) STACK="$2"; shift 2 ;;
    --panel-port) PANEL_PORT="$2"; shift 2 ;;
    --yes) YES="1"; shift ;;
    --dry-run) DRY_RUN="1"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

require_cmd curl
require_cmd tar
require_cmd sha256sum
require_cmd uname

if [[ "${EUID}" -ne 0 && "${INSTALL_DIR}" == "/usr/local/bin" ]]; then
  echo "Veil installer must run as root when installing into /usr/local/bin." >&2
  echo "Run with sudo, or pass --install-dir to a writable directory." >&2
  exit 1
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

(
  cd "${tmpdir}"
  grep "  ${asset}$" checksums.txt | sha256sum -c -
)

tar -xzf "${archive}" -C "${tmpdir}"
if [[ ! -x "${tmpdir}/veil" ]]; then
  chmod +x "${tmpdir}/veil"
fi

mkdir -p "${INSTALL_DIR}"
install -m 0755 "${tmpdir}/veil" "${INSTALL_DIR}/veil"

echo "Installed ${INSTALL_DIR}/veil"

args=(--profile "${PROFILE}" --stack "${STACK}")
if [[ -n "${DOMAIN}" ]]; then args+=(--domain "${DOMAIN}"); fi
if [[ -n "${EMAIL}" ]]; then args+=(--email "${EMAIL}"); fi
if [[ -n "${PORT}" ]]; then args+=(--port "${PORT}"); fi
if [[ -n "${PANEL_PORT}" ]]; then args+=(--panel-port "${PANEL_PORT}"); fi
if [[ -n "${YES}" ]]; then args+=(--yes); else args+=(--interactive); fi
if [[ -n "${DRY_RUN}" ]]; then args+=(--dry-run); fi

exec "${INSTALL_DIR}/veil" install "${args[@]}"
