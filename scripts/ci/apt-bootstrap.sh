#!/usr/bin/env bash
# scripts/ci/apt-bootstrap.sh — make apt usable on GitHub-hosted Ubuntu 24.04.
#
# GitHub's image prefers azure.archive.ubuntu.com via /etc/apt/apt-mirrors.txt.
# That host can stall after "Ign:" without failing, so `apt-get update` burns
# the whole job timeout (lint 20m, test 30m, browser-e2e 25m). Pin Ubuntu to
# archive.ubuntu.com and cap acquire timeouts so a dead mirror fails over.
set -euo pipefail

if [ "$(id -u)" -eq 0 ]; then
  SUDO=""
else
  SUDO="sudo"
fi

${SUDO} tee /etc/apt/apt.conf.d/99veil-ci-timeouts >/dev/null <<'EOF'
Acquire::Retries "3";
Acquire::http::Timeout "20";
Acquire::https::Timeout "20";
Acquire::ftp::Timeout "20";
Acquire::http::Pipeline-Depth "0";
EOF

if [ -f /etc/apt/apt-mirrors.txt ]; then
  ${SUDO} tee /etc/apt/apt-mirrors.txt >/dev/null <<'EOF'
http://archive.ubuntu.com/ubuntu/
http://security.ubuntu.com/ubuntu/
EOF
fi

rewrite_ubuntu_uri() {
  local path="$1"
  if [ ! -f "${path}" ]; then
    return 0
  fi
  ${SUDO} sed -i \
    -e 's|https\?://azure\.archive\.ubuntu\.com/ubuntu|http://archive.ubuntu.com/ubuntu|g' \
    "${path}"
}

rewrite_ubuntu_uri /etc/apt/sources.list
for src in /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; do
  [ -f "${src}" ] || continue
  rewrite_ubuntu_uri "${src}"
done
