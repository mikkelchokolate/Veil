#!/bin/sh
set -eu

# Install the current main-branch commit from source, then hand off to the
# same privileged installer used by the signed release bootstrap.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install-main.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install-main.sh | sh -s -- \
#     --panel-access caddy --domain vpn.example.com --email admin@example.com --yes

OFFICIAL_REPO="mikkelchokolate/Veil"
BRANCH="main"

usage() {
  cat <<USAGE
Veil main-branch installer

Builds the current ${BRANCH} commit from source and installs it with the same
privileged installer handoff as the tagged release bootstrap. This path is not
cosign-signed; use the release installer for verified releases:

  curl -fsSL https://github.com/${OFFICIAL_REPO}/releases/latest/download/install.sh | sh

Install latest ${BRANCH}:

  curl -fsSL https://raw.githubusercontent.com/${OFFICIAL_REPO}/${BRANCH}/scripts/install-main.sh | sh

Options after -- are passed to install-privileged.sh / veil install
(--panel-access, --domain, --email, --yes, --force, ...).

USAGE
}

for argument in "$@"; do
  case "$argument" in
    -h|--help) usage; exit 0 ;;
  esac
done

for command in curl tar sed awk uname mktemp sha256sum python3; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "Required command not found: $command" >&2
    exit 1
  }
done

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
node_arch=""
go_arch=""
case "$arch" in
  x86_64|amd64) go_arch="amd64"; node_arch="x64" ;;
  aarch64|arm64) go_arch="arm64"; node_arch="arm64" ;;
  *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
esac
[ "$os" = "linux" ] || { echo "Unsupported OS: $os" >&2; exit 1; }

work="$(mktemp -d)"
cleanup() { rm -rf "$work"; }
trap cleanup EXIT HUP INT TERM
chmod 0700 "$work"

echo "Resolving origin/${BRANCH}..."
commit_json="$(curl -fsSL "https://api.github.com/repos/${OFFICIAL_REPO}/commits/${BRANCH}")"
sha="$(printf '%s\n' "$commit_json" | sed -n 's/.*"sha"[[:space:]]*:[[:space:]]*"\([0-9a-f]\{40\}\)".*/\1/p' | head -n 1)"
if [ -z "$sha" ] || [ "${#sha}" -ne 40 ]; then
  echo "Could not resolve ${OFFICIAL_REPO}@${BRANCH} commit SHA" >&2
  exit 1
fi
short="$(printf '%s' "$sha" | cut -c1-12)"
echo "Building Veil main@${short}"

echo "Downloading source ${sha}..."
curl -fsSLo "$work/src.tar.gz" "https://github.com/${OFFICIAL_REPO}/archive/${sha}.tar.gz"
src="$work/src"
mkdir -p "$src"
tar -xzf "$work/src.tar.gz" -C "$src" --strip-components=1
if [ ! -f "$src/scripts/install-privileged.sh" ] || [ ! -f "$src/go.mod" ]; then
  echo "Downloaded source is missing install-privileged.sh or go.mod" >&2
  exit 1
fi

# versions.sh is the single source of truth for toolchain pins.
# shellcheck disable=SC1091
. "$src/scripts/ci/versions.sh"

go_ok() {
  command -v go >/dev/null 2>&1 || return 1
  ver="$(go env GOVERSION 2>/dev/null || true)"
  [ -n "$ver" ] || return 1
  case "$ver" in
    go"${CI_GO_VERSION}"*|go1.2[7-9]*|go1.[3-9]*|go[2-9]*) return 0 ;;
    *) return 1 ;;
  esac
}

node_ok() {
  command -v node >/dev/null 2>&1 || return 1
  major="$(node -p "process.versions.node.split('.')[0]" 2>/dev/null || true)"
  [ -n "$major" ] || return 1
  [ "$major" -ge 20 ]
}

if ! go_ok; then
  if [ "$go_arch" != "amd64" ]; then
    echo "Go ${CI_GO_VERSION}+ is required to build from ${BRANCH}; install it or use amd64 (pinned bootstrap checksums cover amd64)." >&2
    exit 1
  fi
  echo "Installing Go ${CI_GO_VERSION}..."
  curl -fsSLo "$work/go.tgz" "https://go.dev/dl/go${CI_GO_VERSION}.linux-${go_arch}.tar.gz"
  printf '%s  %s\n' "$CI_GO_TARBALL_SHA256" "$work/go.tgz" | sha256sum -c - >/dev/null
  tar -xzf "$work/go.tgz" -C "$work"
  export PATH="$work/go/bin:$PATH"
  go_ok || { echo "Bootstrapped Go is not usable" >&2; exit 1; }
fi

if ! node_ok; then
  if [ "$node_arch" != "x64" ]; then
    echo "Node.js ${CI_NODE_VERSION}+ is required to build the Panel; install it or use amd64 (pinned bootstrap checksums cover x64)." >&2
    exit 1
  fi
  command -v xz >/dev/null 2>&1 || {
    echo "Required command not found: xz (needed to unpack Node.js)" >&2
    exit 1
  }
  echo "Installing Node.js ${CI_NODE_VERSION}..."
  node_tarball="node-v${CI_NODE_VERSION}-linux-${node_arch}.tar.xz"
  curl -fsSLo "$work/$node_tarball" "https://nodejs.org/dist/v${CI_NODE_VERSION}/${node_tarball}"
  printf '%s  %s\n' "$CI_NODE_TARBALL_SHA256" "$work/$node_tarball" | sha256sum -c - >/dev/null
  tar -xJf "$work/$node_tarball" -C "$work"
  export PATH="$work/node-v${CI_NODE_VERSION}-linux-${node_arch}/bin:$PATH"
  node_ok || { echo "Bootstrapped Node.js is not usable" >&2; exit 1; }
fi

if ! command -v pnpm >/dev/null 2>&1; then
  if command -v corepack >/dev/null 2>&1; then
    export COREPACK_HOME="$work/corepack"
    mkdir -p "$COREPACK_HOME"
    corepack enable >/dev/null
    corepack prepare "pnpm@${CI_PNPM_VERSION}" --activate
  else
    echo "pnpm ${CI_PNPM_VERSION} is required (enable corepack or install pnpm)" >&2
    exit 1
  fi
fi

echo "Building Panel frontend..."
(
  cd "$src/web"
  pnpm install --frozen-lockfile
  pnpm build
)
if [ ! -f "$src/web/dist/index.html" ]; then
  echo "Frontend build did not produce web/dist/index.html" >&2
  exit 1
fi

echo "Building veil binary..."
(
  cd "$src"
  CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=main-${short} -X main.commit=${sha}" \
    -o "$work/veil" ./cmd/veil
)
if [ ! -f "$work/veil" ] || [ -L "$work/veil" ]; then
  echo "Build did not produce a regular veil binary" >&2
  exit 1
fi

installer="$src/scripts/install-privileged.sh"
installer_digest="$(sha256sum "$installer" | awk '{print $1}')"
binary_digest="$(sha256sum "$work/veil" | awk '{print $1}')"

echo "Installing main@${short} with the privileged installer..."
# No privileged process is started before the built binary and installer
# bytes are hashed and handed to install-privileged.sh's verified path.
if [ "$(id -u)" -eq 0 ]; then
  env \
    VEIL_INSTALLER_SHA256="$installer_digest" \
    VEIL_VERIFIED_BINARY_SHA256="$binary_digest" \
    bash "$installer" --local-bin "$work/veil" "$@"
else
  command -v sudo >/dev/null 2>&1 || {
    echo "sudo is required to install systemd units" >&2
    exit 1
  }
  sudo env \
    VEIL_INSTALLER_SHA256="$installer_digest" \
    VEIL_VERIFIED_BINARY_SHA256="$binary_digest" \
    bash "$installer" --local-bin "$work/veil" "$@"
fi
