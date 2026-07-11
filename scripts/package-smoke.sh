#!/usr/bin/env bash
set -euo pipefail

version_old="${VEIL_SMOKE_OLD_VERSION:-0.0.1}"
version_new="${VEIL_SMOKE_NEW_VERSION:-0.0.2}"
arch="${VEIL_ARCH:-amd64}"
maintainer="${VEIL_MAINTAINER:-Veil CI <veil@users.noreply.github.com>}"
root="${VEIL_SMOKE_ROOT:-dist/package-smoke}"

command -v docker >/dev/null 2>&1 || { echo 'docker is required' >&2; exit 1; }
command -v nfpm >/dev/null 2>&1 || { echo 'nfpm is required' >&2; exit 1; }
test -x dist/veil || { echo 'dist/veil must be built first' >&2; exit 1; }

rm -rf "${root}"
mkdir -p "${root}/old" "${root}/new"

build_packages() {
  local version="$1"
  local target="$2"
  local packager
  export VEIL_VERSION="${version}"
  export VEIL_ARCH="${arch}"
  export VEIL_MAINTAINER="${maintainer}"
  for packager in deb rpm apk; do
    nfpm package --config packaging/nfpm.yaml --packager "${packager}" --target "${target}/"
  done
}

build_packages "${version_old}" "${root}/old"
build_packages "${version_new}" "${root}/new"

repo="$(pwd)"

run_deb_smoke() {
  docker run --rm \
    -v "${repo}/${root}:/packages:ro" \
    debian:bookworm-slim sh -euxc '
      apt-get update
      apt-get install -y ca-certificates
      dpkg -i /packages/old/*.deb
      test -x /usr/local/bin/veil
      test -f /lib/systemd/system/veil.service
      id veil
      mkdir -p /var/lib/veil /etc/veil
      printf state-before-upgrade > /var/lib/veil/state.json
      printf key-before-upgrade > /etc/veil/state.key
      dpkg -i /packages/new/*.deb
      test "$(cat /var/lib/veil/state.json)" = state-before-upgrade
      test "$(cat /etc/veil/state.key)" = key-before-upgrade
      find /var/lib/veil/migration-backups -type f -name state.json -print -quit | grep .
      find /var/lib/veil/migration-backups -type f -name state.key -print -quit | grep .
      /usr/local/bin/veil version
      dpkg -r veil
    '
}

run_rpm_smoke() {
  docker run --rm \
    -v "${repo}/${root}/new:/packages:ro" \
    rockylinux:9 sh -euxc '
      dnf install -y /packages/*.rpm
      test -x /usr/local/bin/veil
      test -f /lib/systemd/system/veil.service
      id veil
      /usr/local/bin/veil version
      dnf remove -y veil
    '
}

run_apk_smoke() {
  docker run --rm \
    -v "${repo}/${root}/new:/packages:ro" \
    alpine:3.23 sh -euxc '
      apk add --allow-untrusted /packages/*.apk
      test -x /usr/local/bin/veil
      test -f /lib/systemd/system/veil.service
      id veil
      /usr/local/bin/veil version
      apk del veil
    '
}

run_deb_smoke
run_rpm_smoke
run_apk_smoke

echo 'Native package install and upgrade smoke tests passed.'
