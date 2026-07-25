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

fetch_verified() { # <url> <file> <sha256>
  curl --fail --silent --show-error --location "$1" -o "$2"
  echo "$3  $2" | sha256sum --check - >/dev/null
}

install_pinned_runtimes() { # <destdir> <veil-binary-for-caddy-build>
  local dest="$1" veil_bin="${2:-}"
  local tmp
  tmp="$(mktemp -d /tmp/veil-pinned-runtimes.XXXXXX)"
  trap 'rm -rf "${tmp}"' RETURN

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

    fetch_verified "https://github.com/SagerNet/sing-box/releases/download/${CI_SINGBOX_TAG}/${CI_SINGBOX_ASSET}" singbox.tar.gz "${CI_SINGBOX_SHA256}"
    mkdir -p singbox && tar -xzf singbox.tar.gz -C singbox
    install -m 0755 "$(find singbox -type f -name sing-box | head -n1)" "${dest}/sing-box"
  )

  # caddy with naive forward_proxy: source-built with product-pinned modules.
  if [ -n "${veil_bin}" ] && [ -x "${veil_bin}" ]; then
    "${veil_bin}" runtime install --only naiveproxy --bin-dir "${dest}"
  else
    local gopath_cache="${tmp}/go"
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
  fi
  "${dest}/caddy" list-modules | grep -Fx http.handlers.forward_proxy >/dev/null

  (cd "${dest}" && printf '[ci] pinned runtimes installed to %s: %s\n' "${dest}" "$(echo *)")
}
