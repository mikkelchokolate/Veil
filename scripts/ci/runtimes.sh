#!/usr/bin/env bash
# scripts/ci/runtimes.sh — install the pinned protocol runtimes into a target
# directory, SHA256-verified BEFORE unpacking/execution. Used by CI jobs when
# the pre-provisioned image runtime dir is absent (e.g. GitHub-hosted runners).
#
# Required CI must NEVER resolve /releases/latest: versions and checksums come
# from versions.sh. The caddy-with-forward_proxy server runtime is source-built
# with the same pins as the product installer; both values are declared below
# by versions.sh rather than restated in this script.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/versions.sh
. "${_script_dir}/versions.sh"

# Optional content-addressed download cache (CI restores it via actions/cache
# keyed on the pinned SHA256 set). Entries are named by their pinned SHA256 and
# are re-verified on every use, so a stale, truncated or tampered entry is
# discarded and re-fetched — the cache can never widen what is accepted, it
# only keeps CI working while an upstream release asset is unavailable.
fetch_verified() { # <url> <file> <sha256>
  local url="$1" file="$2" sha="$3" cache_dir="${CI_RUNTIME_DOWNLOAD_CACHE:-}" cached=""
  if [ -n "${cache_dir}" ]; then
    cached="${cache_dir}/${sha}"
    if [ -f "${cached}" ] && echo "${sha}  ${cached}" | sha256sum --check --status -; then
      cp "${cached}" "${file}"
      echo "${sha}  ${file}" | sha256sum --check - >/dev/null
      printf '[ci] verified cached download %s (sha256 %s)\n' "${file}" "${sha}"
      return 0
    fi
    rm -f "${cached}"
  fi
  curl --fail --silent --show-error --location "${url}" -o "${file}"
  echo "${sha}  ${file}" | sha256sum --check - >/dev/null
  if [ -n "${cached}" ]; then
    mkdir -p "${cache_dir}"
    cp "${file}" "${cached}.tmp.$$"
    mv -f "${cached}.tmp.$$" "${cached}"
  fi
}

# SHA256 pins of every asset install_pinned_runtimes downloads. The CI cache
# key is derived from this list, and a cache is only saved once it is complete.
# Must list exactly the pins passed to fetch_verified in this file
# (TestCIRuntimeDownloadShasMatchFetchSites enforces it).
pinned_runtime_download_shas() {
  printf '%s\n' "${CI_HYSTERIA_SHA256}" "${CI_MITA_SHA256}" "${CI_MIERU_CLIENT_SHA256}" \
    "${CI_NAIVE_CLIENT_SHA256}" "${CI_SINGBOX_SHA256}"
}

pinned_runtime_download_cache_complete() { # <cache-dir>
  local sha
  while IFS= read -r sha; do
    [ -f "$1/${sha}" ] || return 1
    echo "${sha}  $1/${sha}" | sha256sum --check --status - || return 1
  done < <(pinned_runtime_download_shas)
}

caddy_test_runtime_is_pinned() { # <binary>
  [ -x "$1" ] || return 1
  go version -m "$1" | awk -v caddy="${CI_CADDY_VERSION}" -v proxy="${CI_FORWARDPROXY_VERSION}" '
    $1 == "dep" && $2 == "github.com/caddyserver/caddy/v2" && $3 == caddy { caddy_ok = 1 }
    $1 == "=>" && $2 == "github.com/klzgrad/forwardproxy" {
      # Go records a commit pin as a pseudo-version with its 12-digit revision.
      if ($3 == proxy || (length(proxy) == 40 && proxy !~ /[^0-9a-f]/ && $3 ~ ("-" substr(proxy, 1, 12) "$"))) proxy_ok = 1
    }
    END { exit !(caddy_ok && proxy_ok) }
  ' || return 1
  "$1" list-modules 2>/dev/null | grep -Fx http.handlers.forward_proxy >/dev/null
}

singbox_test_runtime_is_pinned() { # <binary>
  [ -x "$1" ] || return 1
  "$1" version 2>/dev/null | awk -v version="${CI_SINGBOX_TAG#v}" '
    NR == 1 && $0 == "sing-box version " version { found = 1 }
    END { exit !found }
  '
}

install_pinned_caddy_binary() { # <destdir> <veil-binary-for-caddy-build>
  local dest="$1" veil_bin="${2:-}"
  mkdir -p "${dest}"
  # Hosted runners require authorization for systemd transient units and
  # restrict unprivileged Bubblewrap namespaces. Enter either sandbox through
  # the runner's explicitly provisioned passwordless sudo. Root-owned Docker
  # CI containers do not take this branch.
  if [ -n "${veil_bin}" ] && [ -x "${veil_bin}" ]; then
    if [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1; then
      sudo -- "${veil_bin}" runtime install --only naiveproxy --bin-dir "${dest}"
    else
      "${veil_bin}" runtime install --only naiveproxy --bin-dir "${dest}"
    fi
  else
    local tmp gopath_cache
    tmp="$(mktemp -d /tmp/veil-pinned-caddy.XXXXXX)"
    gopath_cache="${tmp}/go"
    # A RETURN trap armed here stays set after this function returns and
    # re-fires on the caller's next function return with `tmp` unbound under
    # set -u — disarm it once the build subshell is done.
    trap 'rm -rf "${tmp}"; trap - RETURN' RETURN
    mkdir -p "${tmp}/caddy-build" "${gopath_cache}"
    (
      cd "${tmp}/caddy-build"
      cat > main.go <<'EOF'
package main

import (
	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	_ "github.com/caddyserver/forwardproxy"
)

func main() {
	caddycmd.Main()
}
EOF
      go mod init caddy >/dev/null
      go mod edit -require "github.com/caddyserver/caddy/v2@${CI_CADDY_VERSION}"
      go mod edit -replace "github.com/caddyserver/forwardproxy=github.com/klzgrad/forwardproxy@${CI_FORWARDPROXY_VERSION}"
      go mod tidy
      CGO_ENABLED=0 go build -o "${dest}/caddy" -ldflags='-s -w' -trimpath .
    )
    trap - RETURN
    rm -rf "${tmp}"
  fi
  "${dest}/caddy" list-modules | grep -Fx http.handlers.forward_proxy >/dev/null
}

install_pinned_caddy_for_tests() { # <destdir> <veil-binary-for-caddy-build>
  local dest="$1" veil_bin="${2:-}"
  mkdir -p "${dest}"
  if caddy_test_runtime_is_pinned "${dest}/caddy"; then
    printf '[ci] validated cached Caddy test runtime at %s\n' "${dest}"
    return 0
  fi
  rm -f "${dest}/caddy"
  install_pinned_caddy_binary "${dest}" "${veil_bin}"
  caddy_test_runtime_is_pinned "${dest}/caddy"
  printf '[ci] pinned Caddy test runtime installed to %s\n' "${dest}"
}

# Shared checksum-verified sing-box install — call from a scratch dir; the
# tarball lands in CWD and the verified binary is installed into <dest>.
install_singbox_from_workdir() { # <dest>
  local dest="$1"
  fetch_verified "https://github.com/SagerNet/sing-box/releases/download/${CI_SINGBOX_TAG}/${CI_SINGBOX_ASSET}" singbox.tar.gz "${CI_SINGBOX_SHA256}"
  mkdir -p singbox && tar -xzf singbox.tar.gz -C singbox
  install -m 0755 "$(find singbox -type f -name sing-box | head -n1)" "${dest}/sing-box"
}

# The generated-config catalog validates warp artifacts with `sing-box check`
# (artifact_catalog.go) — and apply now fails closed when that configured
# validator is absent from PATH (issue #686). The test job must therefore
# provision the real pinned binary so warp validations execute instead of
# skipping: a validator that never runs is not evidence.
install_pinned_singbox_for_tests() { # <destdir>
  local dest="$1"
  mkdir -p "${dest}"
  if singbox_test_runtime_is_pinned "${dest}/sing-box"; then
    printf '[ci] validated cached sing-box test runtime at %s\n' "${dest}"
    return 0
  fi
  rm -f "${dest}/sing-box"
  local tmp
  tmp="$(mktemp -d /tmp/veil-pinned-singbox.XXXXXX)"
  # RETURN traps persist past this function's own return — clear it after the
  # install or the next caller's return re-fires it with `tmp` unbound (#686).
  trap 'rm -rf "${tmp}"; trap - RETURN' RETURN
  (
    cd "${tmp}"
    install_singbox_from_workdir "${dest}"
  )
  trap - RETURN
  rm -rf "${tmp}"
  singbox_test_runtime_is_pinned "${dest}/sing-box"
  printf '[ci] pinned sing-box test runtime installed to %s\n' "${dest}"
}

install_pinned_runtimes() { # <destdir> <veil-binary-for-caddy-build>
  local dest="$1" veil_bin="${2:-}"
  local tmp
  tmp="$(mktemp -d /tmp/veil-pinned-runtimes.XXXXXX)"
  # Same RETURN-trap hygiene as install_pinned_caddy_binary: disarm once the
  # fetch subshell finishes so the trap cannot leak to the caller's functions.
  trap 'rm -rf "${tmp}"; trap - RETURN' RETURN

  mkdir -p "${dest}"
  (
    cd "${tmp}"
    fetch_verified "https://github.com/apernet/hysteria/releases/download/${CI_HYSTERIA_TAG}/${CI_HYSTERIA_ASSET}" hysteria "${CI_HYSTERIA_SHA256}"
    install -m 0755 hysteria "${dest}/hysteria"

    fetch_verified "https://github.com/enfein/mieru/releases/download/${CI_MITA_TAG}/${CI_MITA_ASSET}" mita.tar.gz "${CI_MITA_SHA256}"
    tar -xzf mita.tar.gz
    install -m 0755 "$(find . -maxdepth 2 -type f -name mita | head -n1)" "${dest}/mita"

    fetch_verified "https://github.com/enfein/mieru/releases/download/${CI_MIERU_CLIENT_TAG}/${CI_MIERU_CLIENT_ASSET}" mieru.tar.gz "${CI_MIERU_CLIENT_SHA256}"
    mkdir -p mieru-client && tar -xzf mieru.tar.gz -C mieru-client
    install -m 0755 "$(find mieru-client -type f -name mieru | head -n1)" "${dest}/mieru"

    fetch_verified "https://github.com/klzgrad/naiveproxy/releases/download/${CI_NAIVE_CLIENT_TAG}/${CI_NAIVE_CLIENT_ASSET}" naive.tar.xz "${CI_NAIVE_CLIENT_SHA256}"
    mkdir -p naive-client && tar -xJf naive.tar.xz -C naive-client
    install -m 0755 "$(find naive-client -type f -name naive | head -n1)" "${dest}/naive"

    install_singbox_from_workdir "${dest}"
  )
  trap - RETURN
  rm -rf "${tmp}"

  # caddy with naive forward_proxy: source-built with product-pinned modules.
  install_pinned_caddy_binary "${dest}" "${veil_bin}"

  (cd "${dest}" && printf '[ci] pinned runtimes installed to %s: %s\n' "${dest}" "$(echo *)")
}
