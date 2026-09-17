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

# BEGIN_NODE_HELPERS
# Vite 8 (web/package.json) requires Node 20.19+ or 22.12+; 24+ also works.
# Odd majors 21/23 lack the unflagged require(esm) baseline Vite 8 documents.
node_version_supported() {
  ver="${1#v}"
  ver="${ver%%[!0-9.]*}"
  [ -n "$ver" ] || return 1
  major="${ver%%.*}"
  case "$major" in
    ''|*[!0-9]*) return 1 ;;
  esac
  rest=""
  minor=0
  if [ "$major" != "$ver" ]; then
    rest="${ver#*.}"
    minor="${rest%%.*}"
  fi
  case "$minor" in
    ''|*[!0-9]*) return 1 ;;
  esac
  if [ "$major" -ge 24 ]; then
    return 0
  fi
  if [ "$major" -eq 22 ] && [ "$minor" -ge 12 ]; then
    return 0
  fi
  if [ "$major" -eq 20 ] && [ "$minor" -ge 19 ]; then
    return 0
  fi
  return 1
}

node_ok() {
  node_bin="${1:-}"
  if [ -n "$node_bin" ]; then
    [ -x "$node_bin" ] || return 1
  else
    command -v node >/dev/null 2>&1 || return 1
    node_bin="$(command -v node)"
    [ -n "$node_bin" ] || return 1
  fi
  ver="$("$node_bin" -v 2>/dev/null || true)"
  [ -n "$ver" ] || return 1
  node_version_supported "$ver" || return 1
  major="$("$node_bin" -p "process.versions.node.split('.')[0]" 2>/dev/null || true)"
  [ -n "$major" ] || return 1
  case "$major" in
    *[!0-9]*) return 1 ;;
  esac
  return 0
}

diagnose_node() {
  target="${1:-}"
  echo "Node usability diagnostics:" >&2
  echo "PATH=${PATH}" >&2
  echo "command -v node: $(command -v node 2>/dev/null || echo not-found)" >&2
  echo "uname: $(uname -s 2>/dev/null || true) $(uname -m 2>/dev/null || true)" >&2
  if [ -z "$target" ]; then
    target="$(command -v node 2>/dev/null || true)"
  fi
  echo "node binary: ${target:-none}" >&2
  if [ -n "$target" ] && [ -e "$target" ]; then
    ls -l "$target" >&2 || true
    echo "node -v:" >&2
    "$target" -v >&2 || echo "node -v exited $?" >&2
    echo "node -p process.versions.node.split:" >&2
    "$target" -p "process.versions.node.split('.')[0]" >&2 || echo "node -p exited $?" >&2
    if command -v ldd >/dev/null 2>&1; then
      echo "ldd:" >&2
      ldd "$target" >&2 || true
    else
      echo "ldd: not found" >&2
    fi
  else
    echo "node binary is missing" >&2
  fi
}
# END_NODE_HELPERS

# BEGIN_DEPS_HELPERS
# Provision the operating-system runtime libraries a downloaded toolchain links
# against (audit #294). The pinned Node build needs libatomic plus the usual
# C/C++ runtime (libstdc++, libgcc_s, glibc/musl); minimal hosts may ship none
# of them. Missing libraries are mapped to the distro package providing them
# and installed through the detected package manager with a narrowly scoped
# command — never by fetching .so files or mutating loader search paths.
detect_pkg_manager() {
  for manager in apt-get dnf yum pacman zypper apk; do
    if command -v "$manager" >/dev/null 2>&1; then
      echo "$manager"
      return 0
    fi
  done
  return 1
}

pkg_install() {
  manager="$1"
  shift
  sudo=""
  if [ "$(id -u)" -ne 0 ]; then
    if command -v sudo >/dev/null 2>&1; then
      sudo="sudo"
    else
      echo "Installing packages requires root; re-run with sudo or install them manually: $*" >&2
      return 1
    fi
  fi
  case "$manager" in
    apt-get)
      $sudo apt-get update -qq >/dev/null 2>&1 || true
      $sudo apt-get install -y "$@"
      ;;
    dnf)    $sudo dnf -y install "$@" ;;
    yum)    $sudo yum -y install "$@" ;;
    pacman) $sudo pacman -Sy --noconfirm "$@" ;;
    zypper) $sudo zypper -q install -y "$@" ;;
    apk)    $sudo apk add "$@" ;;
    *) return 1 ;;
  esac
}

# Map a missing SONAME to the package providing it for the detected manager.
lib_package_name() {
  soname="$1"
  manager="$2"
  family=""
  case "$soname" in
    libatomic*)   family="atomic" ;;
    libstdc++*)   family="stdcxx" ;;
    libgcc_s*)    family="gcc" ;;
    libc.so.*|libm.so.*|libdl.so.*|libpthread*|ld-linux*|ld-*.so*) family="libc" ;;
    *) return 1 ;;
  esac
  case "$family:$manager" in
    atomic:apt-get) echo "libatomic1" ;;
    atomic:pacman)  echo "gcc-libs" ;;
    atomic:*)       echo "libatomic" ;;
    stdcxx:apt-get) echo "libstdc++6" ;;
    stdcxx:pacman)  echo "gcc-libs" ;;
    stdcxx:zypper)  echo "libstdc++6" ;;
    stdcxx:*)       echo "libstdc++" ;;
    gcc:apt-get)    echo "libgcc-s1" ;;
    gcc:pacman)     echo "gcc-libs" ;;
    gcc:zypper)     echo "libgcc_s1" ;;
    gcc:*)          echo "libgcc" ;;
    libc:apt-get)   echo "libc6" ;;
    libc:apk)       echo "musl" ;;
    libc:*)         echo "glibc" ;;
    *) return 1 ;;
  esac
}

# ensure_runtime_libs installs packages supplying shared libraries the given
# binary is missing. Returns nonzero with an actionable message when the
# dependency cannot be resolved on this host.
ensure_runtime_libs() {
  bin="$1"
  command -v ldd >/dev/null 2>&1 || return 0
  missing="$(ldd "$bin" 2>/dev/null | awk '/not found/ {print $1}' | sort -u)"
  [ -n "$missing" ] || return 0
  manager="$(detect_pkg_manager || true)"
  packages=""
  for lib in $missing; do
    pkg=""
    if [ -n "$manager" ]; then
      pkg="$(lib_package_name "$lib" "$manager" || true)"
    fi
    if [ -z "$pkg" ]; then
      echo "Missing shared library for $bin: $lib" >&2
      echo "Install the package providing $lib with your package manager and re-run." >&2
      return 1
    fi
    case " $packages " in
      *" $pkg "*) ;;
      *) packages="$packages $pkg" ;;
    esac
  done
  if [ -z "$manager" ]; then
    echo "Missing shared libraries for $bin: $missing" >&2
    echo "No supported package manager found; install the packages providing them and re-run." >&2
    return 1
  fi
  echo "Installing toolchain runtime libraries:$packages"
  # shellcheck disable=SC2086 # package list is intentionally word-split
  pkg_install "$manager" $packages
}
# END_DEPS_HELPERS

if ! go_ok; then
  go_sha256=""
  case "$go_arch" in
    amd64) go_sha256="$CI_GO_TARBALL_SHA256" ;;
    arm64) go_sha256="${CI_GO_TARBALL_SHA256_ARM64:-}" ;;
  esac
  if [ -z "$go_sha256" ]; then
    echo "Go ${CI_GO_VERSION}+ is required to build from ${BRANCH}; install it first (no pinned bootstrap checksum for linux-${go_arch})." >&2
    exit 1
  fi
  echo "Installing Go ${CI_GO_VERSION}..."
  curl -fsSLo "$work/go.tgz" "https://go.dev/dl/go${CI_GO_VERSION}.linux-${go_arch}.tar.gz"
  printf '%s  %s\n' "$go_sha256" "$work/go.tgz" | sha256sum -c - >/dev/null
  tar -xzf "$work/go.tgz" -C "$work"
  export PATH="$work/go/bin:$PATH"
  hash -r 2>/dev/null || true
  go_ok || { echo "Bootstrapped Go is not usable" >&2; exit 1; }
fi

if ! node_ok; then
  node_sha256=""
  case "$node_arch" in
    x64)   node_sha256="$CI_NODE_TARBALL_SHA256" ;;
    arm64) node_sha256="${CI_NODE_TARBALL_SHA256_ARM64:-}" ;;
  esac
  if [ -z "$node_sha256" ]; then
    echo "Node.js ${CI_NODE_VERSION}+ is required to build the Panel; install it first (no pinned bootstrap checksum for linux-${node_arch})." >&2
    exit 1
  fi
  command -v xz >/dev/null 2>&1 || {
    echo "Required command not found: xz (needed to unpack Node.js)" >&2
    exit 1
  }
  echo "Installing Node.js ${CI_NODE_VERSION}..."
  node_suffix=""
  if [ -e /lib/ld-musl-x86_64.so.1 ] || [ -e /lib/ld-musl-aarch64.so.1 ]; then
    if [ "$node_arch" != "x64" ]; then
      echo "musl libc on ${node_arch}: there is no official Node.js musl build for this architecture; install Node.js ${CI_NODE_VERSION}+ manually and re-run." >&2
      exit 1
    fi
    node_suffix="-musl"
    node_sha256="${CI_NODE_MUSL_TARBALL_SHA256:-}"
    if [ -z "$node_sha256" ]; then
      echo "musl libc detected; pinned Node musl checksum is missing" >&2
      exit 1
    fi
  fi
  node_tarball="node-v${CI_NODE_VERSION}-linux-${node_arch}${node_suffix}.tar.xz"
  curl -fsSLo "$work/$node_tarball" "https://nodejs.org/dist/v${CI_NODE_VERSION}/${node_tarball}"
  printf '%s  %s\n' "$node_sha256" "$work/$node_tarball" | sha256sum -c - >/dev/null
  tar -xJf "$work/$node_tarball" -C "$work"
  node_bin=""
  for candidate in "$work"/node-v*/bin/node; do
    if [ -f "$candidate" ]; then
      node_bin="$candidate"
      break
    fi
  done
  if [ -z "$node_bin" ] || [ ! -f "$node_bin" ]; then
    echo "Extracted Node.js tarball is missing bin/node (arch=${node_arch} tarball=${node_tarball})" >&2
    diagnose_node "$work/node-v${CI_NODE_VERSION}-linux-${node_arch}${node_suffix}/bin/node"
    echo "Bootstrapped Node.js is not usable" >&2
    exit 1
  fi
  chmod +x "$node_bin" 2>/dev/null || true
  node_bindir="$(dirname "$node_bin")"
  PATH="$node_bindir:$PATH"
  export PATH
  hash -r 2>/dev/null || true
  # Provision OS runtime libraries (libatomic/libstdc++/...) the pinned build
  # links against before launching it (audit #294).
  if ! ensure_runtime_libs "$node_bin"; then
    diagnose_node "$node_bin"
    echo "Bootstrapped Node.js is not usable" >&2
    exit 1
  fi
  if ! node_ok "$node_bin"; then
    diagnose_node "$node_bin"
    echo "Bootstrapped Node.js is not usable" >&2
    exit 1
  fi
fi

# BEGIN_PNPM_BOOTSTRAP
# A leftover pnpm shim can exist on PATH while its interpreter is gone
# (audit #301): `command -v` alone accepts it and the first real invocation
# fails. Reuse an existing pnpm only when it actually executes and reports the
# pinned major; otherwise prepare the pinned pnpm in this installer's private
# location and put it first on PATH without touching the user's shim.
pnpm_ok() {
  command -v pnpm >/dev/null 2>&1 || return 1
  pnpm_bin="$(command -v pnpm)"
  [ -n "$pnpm_bin" ] && [ -x "$pnpm_bin" ] || return 1
  if command -v timeout >/dev/null 2>&1; then
    ver="$(timeout 20 "$pnpm_bin" --version 2>/dev/null || true)"
  else
    ver="$("$pnpm_bin" --version 2>/dev/null || true)"
  fi
  [ -n "$ver" ] || return 1
  case "$ver" in
    "${CI_PNPM_VERSION%%.*}".*) return 0 ;;
    *) return 1 ;;
  esac
}

ensure_pnpm() {
  if pnpm_ok; then
    return 0
  fi
  if command -v corepack >/dev/null 2>&1; then
    COREPACK_HOME="${COREPACK_HOME:-$work/corepack}"
    export COREPACK_HOME
    mkdir -p "$COREPACK_HOME" "$work/bin"
    if corepack enable --install-directory "$work/bin" >/dev/null; then
      PATH="$work/bin:$PATH"
      export PATH
      hash -r 2>/dev/null || true
      corepack prepare "pnpm@${CI_PNPM_VERSION}" --activate
    fi
  fi
  if ! pnpm_ok && command -v npm >/dev/null 2>&1; then
    npm install -g --prefix "$work/pnpm-prefix" "pnpm@${CI_PNPM_VERSION}"
    PATH="$work/pnpm-prefix/bin:$PATH"
    export PATH
    hash -r 2>/dev/null || true
  fi
  if ! pnpm_ok; then
    echo "pnpm ${CI_PNPM_VERSION} is required (enable corepack or install npm/pnpm)" >&2
    exit 1
  fi
}
# END_PNPM_BOOTSTRAP
ensure_pnpm

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
  # A `sudo install-main.sh` invocation builds as root while inheriting
  # SUDO_UID. The verified handoff must describe that root owner, not the
  # original unprivileged caller.
  env \
    SUDO_UID= \
    SUDO_GID= \
    SUDO_USER= \
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
