#!/usr/bin/env bash
set -euo pipefail

OFFICIAL_REPO="mikkelchokolate/Veil"
REPO="${OFFICIAL_REPO}"
VERSION="${VEIL_VERSION:-latest}"
INSTALL_DIR="${VEIL_INSTALL_DIR:-/usr/local/bin}"
EXPECTED_INSTALLER_SHA256="${VEIL_INSTALLER_SHA256:-}"
UNSAFE_DEVELOPMENT=""

verify_installer_bytes() {
  if [[ -z "${EXPECTED_INSTALLER_SHA256}" || ! "${EXPECTED_INSTALLER_SHA256}" =~ ^[0-9a-fA-F]{64}$ ]]; then
    echo "Installer verification metadata is missing or invalid; use the verified bootstrap flow." >&2
    exit 1
  fi
  local actual
  actual="$(sha256sum "${BASH_SOURCE[0]}" | awk '{print $1}')"
  if [[ "${actual,,}" != "${EXPECTED_INSTALLER_SHA256,,}" ]]; then
    echo "Installer verification failed: exact script digest mismatch." >&2
    exit 1
  fi
}

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
  curl -fsSL https://github.com/mikkelchokolate/Veil/releases/latest/download/install.sh | sh
  curl -fsSL https://github.com/mikkelchokolate/Veil/releases/latest/download/install.sh | sh -s -- --profile ru-recommended --panel-access caddy --domain example.com --email admin@example.com

Options:
  Veil install only installs Panel; configure protocols as Panel Inbounds
  --version VERSION    Release tag to install, default latest
  --install-dir DIR    Directory for the veil binary, default /usr/local/bin
  --profile NAME       ru-recommended (default)
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
  --unsafe-development Allow VEIL_REPO/local development overrides after self-verification
  -h, --help           Show this help

Environment:
  VEIL_REPO            Development-only repo override; requires --unsafe-development
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
    "${RUN_BIN}" install "${args[@]}" < /dev/tty
    return $?
  fi
  "${RUN_BIN}" install "${args[@]}"
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
    --unsafe-development) UNSAFE_DEVELOPMENT="1"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

require_cmd sha256sum
verify_installer_bytes
if [[ -n "${VEIL_REPO:-}" ]]; then
  if [[ -z "${UNSAFE_DEVELOPMENT}" ]]; then
    echo "Installer verification rejected VEIL_REPO without --unsafe-development." >&2
    exit 1
  fi
  REPO="${VEIL_REPO}"
fi

RUN_BIN="${INSTALL_DIR}/veil"

require_cmd curl
require_cmd tar
require_cmd sha256sum
require_cmd uname

if [[ -n "${LOCAL_BIN}" && ! -f "${LOCAL_BIN}" ]]; then
  echo "Local binary not found: ${LOCAL_BIN}" >&2
  exit 1
fi
if [[ -n "${LOCAL_BIN}" && -z "${UNSAFE_DEVELOPMENT}" ]]; then
  expected_binary="${VEIL_VERIFIED_BINARY_SHA256:-}"
  if [[ ! "${expected_binary}" =~ ^[0-9a-fA-F]{64}$ ]]; then
    echo "Verified binary handoff metadata is missing." >&2
    exit 1
  fi
  require_cmd python3
  mkdir -p "${INSTALL_DIR}"
  verified_copy="$(VEIL_LOCAL_SOURCE="${LOCAL_BIN}" VEIL_EXPECTED_BINARY_SHA256="${expected_binary}" \
    VEIL_EXPECTED_SOURCE_UID="${SUDO_UID:-$(id -u)}" VEIL_INSTALL_DIR="${INSTALL_DIR}" python3 - <<'PY'
import hashlib, os, stat, tempfile

source = os.environ["VEIL_LOCAL_SOURCE"]
expected = os.environ["VEIL_EXPECTED_BINARY_SHA256"].lower()
expected_uid = int(os.environ["VEIL_EXPECTED_SOURCE_UID"])
install_dir = os.environ["VEIL_INSTALL_DIR"]
parent = os.path.dirname(os.path.abspath(source))
parent_stat = os.lstat(parent)
if not stat.S_ISDIR(parent_stat.st_mode) or parent_stat.st_uid != expected_uid or parent_stat.st_mode & 0o022:
    raise SystemExit("Verified binary source directory ownership/mode is unsafe")
flags = os.O_RDONLY | os.O_CLOEXEC
flags |= getattr(os, "O_NOFOLLOW", 0)
source_fd = os.open(source, flags)
temp_path = ""
try:
    source_stat = os.fstat(source_fd)
    if not stat.S_ISREG(source_stat.st_mode) or source_stat.st_nlink != 1 or source_stat.st_uid != expected_uid:
        raise SystemExit("Verified binary source inode ownership/type/link count is unsafe")
    install_stat = os.stat(install_dir)
    if not stat.S_ISDIR(install_stat.st_mode) or install_stat.st_uid != 0 or install_stat.st_mode & 0o022:
        raise SystemExit("Install directory ownership/mode is unsafe")
    temp_fd, temp_path = tempfile.mkstemp(prefix=".veil-verified-", dir=install_dir)
    digest = hashlib.sha256()
    try:
        while True:
            block = os.read(source_fd, 1024 * 1024)
            if not block:
                break
            digest.update(block)
            view = memoryview(block)
            while view:
                written = os.write(temp_fd, view)
                view = view[written:]
        os.fchmod(temp_fd, 0o755)
        os.fchown(temp_fd, 0, 0)
        os.fsync(temp_fd)
    finally:
        os.close(temp_fd)
    if digest.hexdigest() != expected:
        os.unlink(temp_path)
        raise SystemExit("Verified binary handoff digest mismatch")
    directory_fd = os.open(install_dir, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory_fd)
    finally:
        os.close(directory_fd)
    print(temp_path)
finally:
    os.close(source_fd)
PY
  )"
  LOCAL_BIN="${verified_copy}"
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
	previous_binary=""
	had_previous=""
	if [[ -e "${INSTALL_DIR}/veil" ]]; then
	  previous_binary="$(mktemp "${INSTALL_DIR}/.veil-previous.XXXXXX")"
	  install -m 0755 "${INSTALL_DIR}/veil" "${previous_binary}"
	  had_previous="1"
	fi
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
  if run_veil_install; then
	if [[ -n "${previous_binary:-}" ]]; then rm -f "${previous_binary}"; fi
	if [[ "${LOCAL_BIN}" == "${INSTALL_DIR}"/.veil-verified-* ]]; then rm -f "${LOCAL_BIN}"; fi
	exit 0
  else
	status=$?
  fi
  if [[ -z "${DRY_RUN}" ]]; then
	if [[ -n "${had_previous:-}" ]]; then
	  install -m 0755 "${previous_binary}" "${INSTALL_DIR}/veil"
	else
	  rm -f "${INSTALL_DIR}/veil"
	fi
	rm -f "${previous_binary:-}"
	if [[ "${LOCAL_BIN}" == "${INSTALL_DIR}"/.veil-verified-* ]]; then rm -f "${LOCAL_BIN}"; fi
  fi
  exit "${status}"
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
if [[ "${VERSION}" == "latest" ]]; then
  latest_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "${base_url}/latest")"
  VERSION="${latest_url##*/}"
fi
if [[ ! "${VERSION}" =~ ^v[0-9][A-Za-z0-9._+-]*$ ]]; then
  echo "Refusing non-canonical release tag: ${VERSION}" >&2
  exit 1
fi
if ! command -v cosign >/dev/null 2>&1; then
  echo "cosign is required to verify Veil release signatures; no insecure fallback is available" >&2
  exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required to validate Veil SLSA provenance" >&2
  exit 1
fi

download_url="${base_url}/download/${VERSION}/${asset}"
checksums_url="${base_url}/download/${VERSION}/checksums.txt"
checksums_bundle_url="${base_url}/download/${VERSION}/checksums.txt.bundle"
provenance_url="${base_url}/download/${VERSION}/veil.provenance.json"
provenance_bundle_url="${base_url}/download/${VERSION}/veil.provenance.json.bundle"

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "${tmpdir}"
}
trap cleanup EXIT

archive="${tmpdir}/${asset}"
checksums="${tmpdir}/checksums.txt"
checksums_bundle="${tmpdir}/checksums.txt.bundle"
provenance="${tmpdir}/veil.provenance.json"
provenance_bundle="${tmpdir}/veil.provenance.json.bundle"

echo "Downloading Veil ${VERSION} for ${os}/${arch} from ${REPO}..."
curl -fsSL "${download_url}" -o "${archive}"
curl -fsSL "${checksums_url}" -o "${checksums}"
curl -fsSL "${checksums_bundle_url}" -o "${checksums_bundle}"
curl -fsSL "${provenance_url}" -o "${provenance}"
curl -fsSL "${provenance_bundle_url}" -o "${provenance_bundle}"

identity="https://github.com/${REPO}/.github/workflows/release.yml@refs/tags/${VERSION}"
cosign verify-blob --bundle "${checksums_bundle}" \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  --certificate-identity="${identity}" \
  "${checksums}"
cosign verify-blob --bundle "${provenance_bundle}" \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com \
  --certificate-identity="${identity}" \
  "${provenance}"

ASSET="${asset}" ARCHIVE="${archive}" CHECKSUMS="${checksums}" PROVENANCE="${provenance}" \
REPOSITORY="${REPO}" RELEASE_TAG="${VERSION}" WORKFLOW_IDENTITY="${identity}" python3 - <<'PY'
import hashlib, json, os

def digest(path):
    with open(path, "rb") as handle:
        return hashlib.sha256(handle.read()).hexdigest()

with open(os.environ["PROVENANCE"], "r", encoding="utf-8") as handle:
    statement = json.load(handle)
if statement.get("_type") != "https://in-toto.io/Statement/v1":
    raise SystemExit("invalid in-toto statement type")
if statement.get("predicateType") != "https://slsa.dev/provenance/v1":
    raise SystemExit("invalid SLSA provenance predicate")
predicate = statement.get("predicate", {})
build = predicate.get("buildDefinition", {})
params = build.get("externalParameters", {})
if params.get("repository") != "https://github.com/" + os.environ["REPOSITORY"]:
    raise SystemExit("provenance repository mismatch")
if params.get("ref") != "refs/tags/" + os.environ["RELEASE_TAG"]:
    raise SystemExit("provenance tag/ref mismatch")
if predicate.get("runDetails", {}).get("builder", {}).get("id") != os.environ["WORKFLOW_IDENTITY"]:
    raise SystemExit("provenance workflow identity mismatch")
subjects = {item.get("name"): item.get("digest", {}).get("sha256") for item in statement.get("subject", [])}
expected = {
    os.environ["ASSET"]: digest(os.environ["ARCHIVE"]),
    "checksums.txt": digest(os.environ["CHECKSUMS"]),
}
for name, value in expected.items():
    if subjects.get(name) != value:
        raise SystemExit("provenance subject digest mismatch for " + name)
PY

# Trust checksums only after independent cosign identity and SLSA provenance verification.
(
  cd "${tmpdir}"
  count=$(awk -v asset="${asset}" '($2 == asset || $2 == "./" asset) { count++ } END { print count+0 }' checksums.txt)
  if [[ "${count}" -ne 1 ]]; then
    echo "expected exactly one checksum for ${asset} in checksums.txt, got ${count}" >&2
    exit 1
  fi
  awk -v asset="${asset}" '($2 == asset || $2 == "./" asset) { print }' checksums.txt | sha256sum -c -
)

tar -xzf "${archive}" --no-same-owner -C "${tmpdir}" veil
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
