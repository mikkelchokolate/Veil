#!/usr/bin/env bash
# scripts/ci/apt-bootstrap.sh — make apt usable on GitHub-hosted Ubuntu 24.04.
#
# GitHub's image prefers azure.archive.ubuntu.com via /etc/apt/apt-mirrors.txt.
# That host can stall after "Ign:" without failing, so `apt-get update` burns
# the whole job timeout (lint 20m, test 30m, browser-e2e 25m). Pin Ubuntu to
# the canonical archive and cap acquire timeouts so a dead mirror fails over.
#
# Mirror hosts are architecture-specific (#437): archive/security.ubuntu.com
# only carry amd64/i386; arm64 and other ports architectures live on
# ports.ubuntu.com/ubuntu-ports. Forcing archive.ubuntu.com on an arm64
# runner fetches the wrong archive or fails outright.
set -euo pipefail

if [ "$(id -u)" -eq 0 ]; then
  SUDO=""
else
  SUDO="sudo"
fi

case "$(dpkg --print-architecture 2>/dev/null || uname -m)" in
  arm64|aarch64)
    APT_MIRROR_URIS="http://ports.ubuntu.com/ubuntu-ports/"
    APT_REWRITE_TARGET="http://ports.ubuntu.com/ubuntu-ports"
    ;;
  *)
    APT_MIRROR_URIS="http://archive.ubuntu.com/ubuntu/
http://security.ubuntu.com/ubuntu/"
    APT_REWRITE_TARGET="http://archive.ubuntu.com/ubuntu"
    ;;
esac

${SUDO} tee /etc/apt/apt.conf.d/99veil-ci-timeouts >/dev/null <<'EOF'
Acquire::Retries "3";
Acquire::http::Timeout "20";
Acquire::https::Timeout "20";
Acquire::ftp::Timeout "20";
Acquire::http::Pipeline-Depth "0";
EOF

if [ -f /etc/apt/apt-mirrors.txt ]; then
  printf '%s\n' "${APT_MIRROR_URIS}" | ${SUDO} tee /etc/apt/apt-mirrors.txt >/dev/null
fi

rewrite_ubuntu_uri() {
  local path="$1"
  if [ ! -f "${path}" ]; then
    return 0
  fi
  ${SUDO} sed -i \
    -e "s|https\?://azure\.archive\.ubuntu\.com/ubuntu|${APT_REWRITE_TARGET}|g" \
    "${path}"
}

rewrite_ubuntu_uri /etc/apt/sources.list
for src in /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; do
  [ -f "${src}" ] || continue
  rewrite_ubuntu_uri "${src}"
done
