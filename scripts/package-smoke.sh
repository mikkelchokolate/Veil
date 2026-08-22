#!/usr/bin/env bash
set -euo pipefail

version_old="${VEIL_SMOKE_OLD_VERSION:-0.0.1}"
version_new="${VEIL_SMOKE_NEW_VERSION:-0.0.2}"
binary_version="${VEIL_SMOKE_BINARY_VERSION:-package-smoke}"
goarch="${VEIL_GOARCH:-amd64}"
arch="${VEIL_ARCH:-amd64}"
maintainer="${VEIL_MAINTAINER:-Veil CI <veil@users.noreply.github.com>}"
root="${VEIL_SMOKE_ROOT:-dist/package-smoke}"

command -v docker >/dev/null 2>&1 || { echo 'docker is required' >&2; exit 1; }
command -v nfpm >/dev/null 2>&1 || { echo 'nfpm is required' >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo 'go is required' >&2; exit 1; }

# Native packages must carry one portable Linux binary. Rebuild it here so the
# package gate cannot accidentally validate a host-linked glibc executable that
# fails at runtime in Alpine despite packaging successfully.
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH="${goarch}" \
  go build -trimpath -ldflags "-s -w -X main.version=${binary_version}" -o dist/veil ./cmd/veil

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
    -e EXPECTED_BINARY_VERSION="${binary_version}" \
    -v "${repo}/${root}:/packages:ro" \
    debian:bookworm-slim sh -euxc '
      apt-get update
      apt-get install -y ca-certificates
      dpkg -i /packages/old/*.deb
      test -x /usr/local/bin/veil
      for unit in veil.service veil-helper.service veil-helper.socket veil-backup.service veil-backup.timer veil-caddy.service veil-hysteria2@.service veil-mieru.service veil-olcrtc@.service veil-warp.service; do
        test -f "/lib/systemd/system/$unit"
      done
      test -f /etc/sysctl.d/99-veil-quic.conf
      id veil
      /usr/local/bin/veil version | grep -F "$EXPECTED_BINARY_VERSION"

      printf state-before-upgrade > /var/lib/veil/state.json
      printf sessions-before-upgrade > /var/lib/veil/sessions.json
      printf key-before-upgrade > /etc/veil/state.key
      printf env-before-upgrade > /etc/veil/veil.env
      chmod 0644 /var/lib/veil/state.json /var/lib/veil/sessions.json /etc/veil/state.key /etc/veil/veil.env

      dpkg -i /packages/new/*.deb
      test "$(cat /var/lib/veil/state.json)" = state-before-upgrade
      test "$(cat /var/lib/veil/sessions.json)" = sessions-before-upgrade
      test "$(cat /etc/veil/state.key)" = key-before-upgrade
      test "$(cat /etc/veil/veil.env)" = env-before-upgrade
      test "$(stat -c "%U:%G %a" /etc/veil)" = "root:veil 750"
      test "$(stat -c "%U:%G %a" /var/lib/veil)" = "veil:veil 750"
      test "$(stat -c "%U:%G %a" /var/lib/veil/state.json)" = "veil:veil 600"
      test "$(stat -c "%U:%G %a" /var/lib/veil/sessions.json)" = "veil:veil 600"
      test "$(stat -c "%U:%G %a" /etc/veil/state.key)" = "root:veil 640"
      test "$(stat -c "%U:%G %a" /etc/veil/veil.env)" = "root:veil 640"
      for file in state.json sessions.json state.key veil.env; do
        find /var/lib/veil/migration-backups -type f -name "$file" -print -quit | grep .
      done
      /usr/local/bin/veil version | grep -F "$EXPECTED_BINARY_VERSION"

      dpkg -r veil
      test ! -e /usr/local/bin/veil
      test "$(cat /var/lib/veil/state.json)" = state-before-upgrade
      test "$(cat /etc/veil/state.key)" = key-before-upgrade
      dpkg -i /packages/new/*.deb
      /usr/local/bin/veil version | grep -F "$EXPECTED_BINARY_VERSION"
    '
}

run_rpm_smoke() {
  docker run --rm \
    -e EXPECTED_BINARY_VERSION="${binary_version}" \
    -v "${repo}/${root}:/packages:ro" \
    rockylinux:9 sh -euxc '
      dnf install -y /packages/old/*.rpm
      test -x /usr/local/bin/veil
      test -f /lib/systemd/system/veil.service
      test -f /etc/sysctl.d/99-veil-quic.conf
      id veil
      /usr/local/bin/veil version | grep -F "$EXPECTED_BINARY_VERSION"

      printf state-before-upgrade > /var/lib/veil/state.json
      printf key-before-upgrade > /etc/veil/state.key
      chmod 0644 /var/lib/veil/state.json /etc/veil/state.key
      dnf upgrade -y /packages/new/*.rpm
      test "$(cat /var/lib/veil/state.json)" = state-before-upgrade
      test "$(cat /etc/veil/state.key)" = key-before-upgrade
      test "$(stat -c "%U:%G %a" /var/lib/veil/state.json)" = "veil:veil 600"
      test "$(stat -c "%U:%G %a" /etc/veil/state.key)" = "root:veil 640"
      /usr/local/bin/veil version | grep -F "$EXPECTED_BINARY_VERSION"

      dnf remove -y veil
      test ! -e /usr/local/bin/veil
      test "$(cat /var/lib/veil/state.json)" = state-before-upgrade
      test "$(cat /etc/veil/state.key)" = key-before-upgrade
      dnf install -y /packages/new/*.rpm
      /usr/local/bin/veil version | grep -F "$EXPECTED_BINARY_VERSION"
    '
}

run_apk_smoke() {
  docker run --rm \
    -e EXPECTED_BINARY_VERSION="${binary_version}" \
    -v "${repo}/${root}:/packages:ro" \
    alpine:3.23 sh -euxc '
      apk add --allow-untrusted /packages/old/*.apk
      test -x /usr/local/bin/veil
      test -f /lib/systemd/system/veil.service
      test -f /etc/sysctl.d/99-veil-quic.conf
      id veil
      /usr/local/bin/veil version | grep -F "$EXPECTED_BINARY_VERSION"

      printf state-before-upgrade > /var/lib/veil/state.json
      printf key-before-upgrade > /etc/veil/state.key
      chmod 0644 /var/lib/veil/state.json /etc/veil/state.key
      apk add --allow-untrusted --upgrade /packages/new/*.apk
      test "$(cat /var/lib/veil/state.json)" = state-before-upgrade
      test "$(cat /etc/veil/state.key)" = key-before-upgrade
      test "$(stat -c "%U:%G %a" /var/lib/veil/state.json)" = "veil:veil 600"
      test "$(stat -c "%U:%G %a" /etc/veil/state.key)" = "root:veil 640"
      /usr/local/bin/veil version | grep -F "$EXPECTED_BINARY_VERSION"

      apk del veil
      test ! -e /usr/local/bin/veil
      test "$(cat /var/lib/veil/state.json)" = state-before-upgrade
      test "$(cat /etc/veil/state.key)" = key-before-upgrade
      apk add --allow-untrusted /packages/new/*.apk
      /usr/local/bin/veil version | grep -F "$EXPECTED_BINARY_VERSION"
    '
}

run_symlink_refusal_smoke() {
  docker run --rm \
    -v "${repo}/${root}:/packages:ro" \
    debian:bookworm-slim sh -euxc '
      apt-get update
      apt-get install -y ca-certificates
      dpkg -i /packages/old/*.deb
      printf untouched > /tmp/state-target
      rm -f /var/lib/veil/state.json
      ln -s /tmp/state-target /var/lib/veil/state.json
      if dpkg -i /packages/new/*.deb; then
        echo "upgrade unexpectedly accepted a symlinked managed state file" >&2
        exit 1
      fi
      test -L /var/lib/veil/state.json
      test "$(cat /tmp/state-target)" = untouched
    '
}

run_deb_smoke
run_rpm_smoke
run_apk_smoke
run_symlink_refusal_smoke

echo 'Native package install, upgrade, reinstall, permissions, and migration safety smoke tests passed.'
