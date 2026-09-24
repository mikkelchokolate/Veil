#!/usr/bin/env bash
# scripts/package-smoke.sh — native package (.deb/.rpm/.apk) lifecycle smoke.
#
# Each distro leg installs the OLD package, upgrades to the NEW one, removes,
# and reinstalls — asserting the real maintainer-script contract each step:
#   - payload files (binary, packaged units, QUIC sysctl drop-in) land/leave
#   - service accounts (veil, veil-proxy, veil-mita, veil ∈ veil-proxy ∩ veil-mita) exist
#   - postinstall's systemctl calls are observed via a stub (daemon-reload,
#     enable veil-helper.socket on install; try-restart — no disable — on
#     upgrade)
#   - state, credentials, and permissions survive upgrade/remove
#   - DEB additionally covers dpkg -P purge; a fail-closed leg proves
#     postinstall aborts when a live systemctl call fails and stays quiet
#     when systemd is not the running init
#
# Distro images are pinned by manifest digest in scripts/ci/versions.sh so the
# smoke cannot drift (issue #394).
#
# Release mode: VEIL_SMOKE_PREBUILT_DIR points at the shipped nfpm artifacts
# (downloaded from the release job's artifact); those become the NEW packages
# so the smoke validates exactly what will be published (issue #396).
set -euo pipefail

version_old="${VEIL_SMOKE_OLD_VERSION:-0.0.1}"
version_new="${VEIL_SMOKE_NEW_VERSION:-0.0.2}"
binary_version="${VEIL_SMOKE_BINARY_VERSION:-package-smoke}"
# What `veil version` must print for the NEW packages. In release mode the
# shipped binary carries the tag; locally both packages carry binary_version.
expected_binary_version="${VEIL_SMOKE_EXPECTED_BINARY_VERSION:-${binary_version}}"
goarch="${VEIL_GOARCH:-amd64}"
arch="${VEIL_ARCH:-amd64}"
maintainer="${VEIL_MAINTAINER:-Veil CI <veil@users.noreply.github.com>}"
root="${VEIL_SMOKE_ROOT:-dist/package-smoke}"
prebuilt_dir="${VEIL_SMOKE_PREBUILT_DIR:-}"

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/versions.sh
. "${_script_dir}/ci/versions.sh"

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

# Shared in-container assertions and fixtures. Kept in ONE file mounted into
# every distro leg so DEB/RPM/APK cannot silently drift to weaker coverage
# (issues #395, #505). POSIX sh — must also run under busybox ash.
cat > "${root}/smoke-asserts.sh" <<'ASSERTS'
#!/bin/sh
# package-smoke shared helpers — runs INSIDE the target distro container.
# Usage: smoke-asserts.sh <phase> [expected-binary-version]
# Phases:
#   setup-systemd-stub       write recording systemctl stub + /run marker
#   setup-systemd-stub-fail  same, but the stub fails every call
#   write-fixtures           sentinel state files + permissions to migrate
#   plant-legacy-caddy       pre-consolidation veil-caddy@ instance + want
#   post-install VERSION     payload, accounts, version, install systemctl log
#   post-upgrade VERSION     payload, version, sentinels, perms, backups, log
#   post-remove|post-purge   payload gone, operator state kept
set -eu
phase="${1:?phase required}"
fail() { echo "package-smoke assert failed (${phase}): $*" >&2; exit 1; }

UNITS="veil.service veil-helper.service veil-helper.socket veil-backup.service veil-backup.timer veil-caddy.service veil-hysteria2@.service veil-mieru.service veil-olcrtc@.service veil-warp.service"
SYSTEMCTL_LOG=/tmp/systemctl.log

write_stub() {
  rc="$1"
  cat > /usr/bin/systemctl <<EOF
#!/bin/sh
echo "systemctl \$*" >> ${SYSTEMCTL_LOG}
# Report representative instances for Veil's template globs so the
# per-instance stop/disable sweep in preremove (stop_disable_matching_units)
# is actually exercised — a stub that always lists nothing would leave the
# hysteria2@/olcrtc@/legacy-caddy@ remove path unasserted (issue #683).
if [ "\$1" = "list-units" ] || [ "\$1" = "list-unit-files" ]; then
  for arg in "\$@"; do
    case "\$arg" in
      'veil-hysteria2@*.service') echo 'veil-hysteria2@ci.service' ;;
      'veil-olcrtc@*.service')    echo 'veil-olcrtc@ci.service' ;;
      'veil-caddy@*.service')     echo 'veil-caddy@legacy.service' ;;
    esac
  done
fi
exit ${rc}
EOF
  chmod +x /usr/bin/systemctl
  : > "${SYSTEMCTL_LOG}"
}

assert_payload_present() {
  [ -x /usr/local/bin/veil ] || fail "veil binary missing or not executable"
  for unit in $UNITS; do
    [ -f "/lib/systemd/system/$unit" ] || fail "packaged unit $unit missing"
  done
  [ -f /etc/sysctl.d/99-veil-quic.conf ] || fail "QUIC sysctl drop-in missing"
}

assert_payload_absent() {
  [ ! -e /usr/local/bin/veil ] || fail "veil binary still present after remove"
  for dir in /lib/systemd/system /usr/lib/systemd/system /etc/systemd/system; do
    for unit in $UNITS; do
      [ ! -e "$dir/$unit" ] || fail "unit $unit left in $dir after remove"
    done
    # Legacy per-inbound Caddy leftovers must not survive remove either
    # (issue #375): unit files in vendor dirs and enablement wants links.
    for leftover in "$dir"/veil-caddy@*.service "$dir"/multi-user.target.wants/veil-caddy@*.service; do
      if [ -e "$leftover" ] || [ -L "$leftover" ]; then
        fail "legacy caddy instance left at $leftover after remove"
      fi
    done
  done
  [ ! -e /etc/sysctl.d/99-veil-quic.conf ] || fail "QUIC sysctl drop-in left after remove"
}

assert_accounts() {
  # The package owns the service accounts AND the veil-proxy group membership
  # (issues #324/#325); the smoke must see all of them, not just `id veil`
  # (issue #478).
  id veil >/dev/null 2>&1 || fail "veil user missing"
  id veil-proxy >/dev/null 2>&1 || fail "veil-proxy user missing"
  id veil-mita >/dev/null 2>&1 || fail "veil-mita user missing"
  id -nG veil 2>/dev/null | tr ' ' '\n' | grep -qx veil-proxy \
    || fail "veil is not a member of the veil-proxy group"
  # The panel reaches the mita appctl UDS through the veil-mita group; the
  # shared edge account must NOT be a member or the isolation is meaningless
  # (issue #624).
  id -nG veil 2>/dev/null | tr ' ' '\n' | grep -qx veil-mita \
    || fail "veil is not a member of the veil-mita group"
  if id -nG veil-proxy 2>/dev/null | tr ' ' '\n' | grep -qx veil-mita; then
    fail "veil-proxy must not be a member of the veil-mita group"
  fi
}

assert_version() {
  /usr/local/bin/veil version | grep -F "$1" >/dev/null \
    || fail "veil version does not contain '$1'"
}

assert_install_systemctl() {
  # postinstall must daemon-reload and enable the helper socket on every
  # install path (issue #499).
  [ -f "$SYSTEMCTL_LOG" ] || fail "systemctl stub log missing"
  grep -qxF 'systemctl daemon-reload' "$SYSTEMCTL_LOG" || fail "postinstall did not daemon-reload"
  grep -qxF 'systemctl enable veil-helper.socket' "$SYSTEMCTL_LOG" \
    || fail "postinstall did not enable veil-helper.socket"
}

assert_upgrade_systemctl() {
  # Upgrade must reload and restart units so the new binary takes over, and
  # must never disable them (issues #479, #494). The bulk try-restart line
  # must name EVERY managed non-template unit — grepping for one unit name
  # passes even when the call drops the rest (issue #752).
  [ -f "$SYSTEMCTL_LOG" ] || fail "systemctl stub log missing"
  grep -qxF 'systemctl daemon-reload' "$SYSTEMCTL_LOG" || fail "upgrade postinstall did not daemon-reload"
  restart_line="$(grep 'try-restart' "$SYSTEMCTL_LOG" | grep 'veil\.service' | head -n 1)"
  [ -n "$restart_line" ] || fail "upgrade postinstall issued no try-restart"
  for unit in veil.service veil-helper.service veil-helper.socket \
      veil-caddy.service veil-mieru.service veil-warp.service \
      veil-backup.service veil-backup.timer; do
    case " $restart_line " in
      *" $unit "*) ;;
      *) fail "bulk try-restart call missing $unit: $restart_line" ;;
    esac
  done
  # Template units restart per concrete instance enumerated via list-units —
  # a '@*' glob restart would be a no-op for loaded instances (issue #776).
  # The stub reports @ci instances so both the enumeration and the restart
  # land in the log.
  grep -q 'systemctl list-units .*veil-hysteria2@\*\.service.*veil-olcrtc@\*\.service' "$SYSTEMCTL_LOG" \
    || fail "upgrade postinstall did not enumerate template instances"
  for unit in veil-hysteria2@ci.service veil-olcrtc@ci.service; do
    grep -qxF "systemctl try-restart $unit" "$SYSTEMCTL_LOG" \
      || fail "upgrade postinstall did not try-restart $unit"
  done
  grep -qxF 'systemctl enable veil-helper.socket' "$SYSTEMCTL_LOG" \
    || fail "upgrade postinstall did not re-enable veil-helper.socket"
  if grep -E "(^|[[:space:]])disable veil(\.service)?([[:space:]]|$)" "$SYSTEMCTL_LOG"; then
    echo "package upgrade disabled veil.service" >&2
    cat "$SYSTEMCTL_LOG" >&2
    exit 1
  fi
  if grep -E "(^|[[:space:]])disable veil-helper\.socket([[:space:]]|$)" "$SYSTEMCTL_LOG"; then
    echo "package upgrade disabled veil-helper.socket" >&2
    cat "$SYSTEMCTL_LOG" >&2
    exit 1
  fi
}

assert_remove_systemctl() {
  # The remove path must stop AND disable every managed unit before the
  # payload is deleted (issue #683). preremove's per-call `|| true` only
  # tolerates "unit not loaded"/"systemd not running" — a preremove that
  # never asks systemctl at all is a silent no-op, which this gate catches:
  # with the stub in place every issued call lands in SYSTEMCTL_LOG.
  [ -f "$SYSTEMCTL_LOG" ] || fail "systemctl stub log missing"
  for unit in veil.service veil-helper.service veil-helper.socket \
      veil-backup.service veil-backup.timer veil-caddy.service \
      veil-mieru.service veil-warp.service; do
    grep -qxF "systemctl stop $unit" "$SYSTEMCTL_LOG" \
      || fail "preremove did not stop $unit"
    grep -qxF "systemctl disable $unit" "$SYSTEMCTL_LOG" \
      || fail "preremove did not disable $unit"
  done
  # Template instances are enumerated via list-units/list-unit-files — the
  # stub reports representative ones (veil-*@ci / veil-caddy@legacy) so the
  # per-instance sweep, including the legacy caddy cleanup (issue #375), is
  # asserted rather than assumed.
  for unit in veil-hysteria2@ci.service veil-olcrtc@ci.service veil-caddy@legacy.service; do
    grep -qxF "systemctl stop $unit" "$SYSTEMCTL_LOG" \
      || fail "preremove did not stop template instance $unit"
    grep -qxF "systemctl disable $unit" "$SYSTEMCTL_LOG" \
      || fail "preremove did not disable template instance $unit"
  done
}

assert_state_sentinels() {
  [ "$(cat /var/lib/veil/state.json)" = state-before-upgrade ] || fail "state.json changed"
  [ "$(cat /var/lib/veil/sessions.json)" = sessions-before-upgrade ] || fail "sessions.json changed"
  [ "$(cat /etc/veil/state.key)" = key-before-upgrade ] || fail "state.key changed"
  [ "$(cat /etc/veil/veil.env)" = env-before-upgrade ] || fail "veil.env changed"
  [ "$(cat /etc/veil/panel/tls.key)" = panel-tls-key ] || fail "panel tls.key changed"
}

assert_permissions() {
  [ "$(stat -c "%U:%G %a" /etc/veil)" = "root:veil 751" ] || fail "/etc/veil owner/mode"
  [ "$(stat -c "%U:%G %a" /var/lib/veil)" = "veil:veil 750" ] || fail "/var/lib/veil owner/mode"
  [ "$(stat -c "%U:%G %a" /var/lib/veil/state.json)" = "veil:veil 600" ] || fail "state.json owner/mode"
  [ "$(stat -c "%U:%G %a" /var/lib/veil/sessions.json)" = "veil:veil 600" ] || fail "sessions.json owner/mode"
  [ "$(stat -c "%U:%G %a" /etc/veil/state.key)" = "root:veil 640" ] || fail "state.key owner/mode"
  [ "$(stat -c "%U:%G %a" /etc/veil/veil.env)" = "root:veil 640" ] || fail "veil.env owner/mode"
  [ "$(stat -c "%U:%G %a" /etc/veil/panel)" = "root:veil-proxy 750" ] || fail "panel dir owner/mode"
  [ "$(stat -c "%U:%G %a" /etc/veil/panel/tls.key)" = "root:veil-proxy 640" ] || fail "panel tls.key owner/mode"
  # Helper socket parent: root-owned traverse-only so no veil-uid process can
  # replace the socket (audit #514/#523).
  [ "$(stat -c "%U:%G %a" /run/veil)" = "root:root 711" ] || fail "/run/veil owner/mode"
  # Runtime-readable trees: root:veil-proxy 0750 (audit #466/#525/#531).
  for dir in /etc/veil/generated /etc/veil/tls /etc/veil/certs /etc/veil/www; do
    [ "$(stat -c "%U:%G %a" "$dir")" = "root:veil-proxy 750" ] || fail "$dir owner/mode"
  done
  # Caddy and Mita state directories re-owned for their service units
  # (#497/#623/#624): systemd never re-owns an existing StateDirectory.
  [ "$(stat -c "%U:%G" /var/lib/caddy)" = "veil-proxy:veil-proxy" ] || fail "/var/lib/caddy owner"
  [ "$(stat -c "%U:%G %a" /var/lib/mita)" = "veil-mita:veil-mita 700" ] || fail "/var/lib/mita owner/mode"
}

assert_unit_hardening() {
  # Shipped units must carry the privilege-boundary contract, not just render
  # it under test (audit #507/#508/#515).
  local unitdir unit
  for unitdir in /lib/systemd/system /usr/lib/systemd/system; do
    [ -f "$unitdir/veil-caddy.service" ] && break
  done
  for unit in veil-caddy.service veil-hysteria2@.service veil-olcrtc@.service veil-warp.service; do
    grep -qxF 'User=veil-proxy' "$unitdir/$unit" || fail "$unit User"
    grep -qxF 'Group=veil-proxy' "$unitdir/$unit" || fail "$unit Group"
    # Exact directive — a reordered, partial, or widened path list means the
    # edge unit sees something it must not (issue #816).
    grep -qxF 'InaccessiblePaths=/run/veil/helper.sock /var/lib/veil' "$unitdir/$unit" \
      || fail "$unit InaccessiblePaths"
    grep -qxF 'CapabilityBoundingSet=CAP_NET_BIND_SERVICE' "$unitdir/$unit" || fail "$unit CapabilityBoundingSet"
    grep -qxF 'AmbientCapabilities=CAP_NET_BIND_SERVICE' "$unitdir/$unit" || fail "$unit AmbientCapabilities"
  done
  # veil-mieru.service runs as the dedicated veil-mita identity (issue #624):
  # its appctl UDS is a control plane, so it must not share the veil-proxy
  # edge uid/gid — only the supplementary group for generated-config reads.
  # Every assertion is an exact-line match: SupplementaryGroups=.*veil-proxy
  # would also accept a hypothetical 'SupplementaryGroups=aveil-proxy' or an
  # extra group that widens the socket's reach (issue #754).
  for want in 'User=veil-mita' 'Group=veil-mita' 'SupplementaryGroups=veil-proxy' \
      'RuntimeDirectory=veil-mieru' 'RuntimeDirectoryMode=0750' 'UMask=0007' \
      'CapabilityBoundingSet=CAP_NET_BIND_SERVICE' 'AmbientCapabilities=CAP_NET_BIND_SERVICE' \
      'InaccessiblePaths=/run/veil/helper.sock /var/lib/veil'; do
    grep -qxF "$want" "$unitdir/veil-mieru.service" || fail "veil-mieru.service missing $want"
  done
  # The panel carries NO capabilities — an ambient/bounding cap on the
  # broadest-reach unit is a privilege-boundary regression (issue #754).
  grep -qxF 'User=veil' "$unitdir/veil.service" || fail "veil.service User"
  grep -qxF 'Group=veil' "$unitdir/veil.service" || fail "veil.service Group"
  grep -qxF 'CapabilityBoundingSet=' "$unitdir/veil.service" \
    || fail "veil.service CapabilityBoundingSet must be empty"
  grep -qxF 'AmbientCapabilities=' "$unitdir/veil.service" \
    || fail "veil.service AmbientCapabilities must be empty"
  # The privileged helper is root with the exact cap set it needs — no more
  # (issue #816).
  grep -qxF 'User=root' "$unitdir/veil-helper.service" || fail "veil-helper.service User"
  grep -qxF 'Group=root' "$unitdir/veil-helper.service" || fail "veil-helper.service Group"
  grep -qxF 'CapabilityBoundingSet=CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_CHOWN CAP_FOWNER CAP_NET_ADMIN CAP_NET_RAW' \
    "$unitdir/veil-helper.service" || fail "veil-helper.service CapabilityBoundingSet"
  grep -qxF 'AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW' "$unitdir/veil-helper.service" \
    || fail "veil-helper.service AmbientCapabilities"
  # The backup unit is root but tightly bounded — its caps cover backup
  # member reads, nothing else (issue #816).
  grep -qxF 'User=root' "$unitdir/veil-backup.service" || fail "veil-backup.service User"
  grep -qxF 'Group=root' "$unitdir/veil-backup.service" || fail "veil-backup.service Group"
  grep -qxF 'CapabilityBoundingSet=CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH' \
    "$unitdir/veil-backup.service" || fail "veil-backup.service CapabilityBoundingSet"
  grep -qxF 'ReadWritePaths=/var/lib/veil' "$unitdir/veil-backup.service" \
    || fail "veil-backup.service ReadWritePaths"
  grep -qxF 'ProtectSystem=strict' "$unitdir/veil-backup.service" \
    || fail "veil-backup.service ProtectSystem"
  grep -qxF 'SocketUser=root' "$unitdir/veil-helper.socket" || fail "socket user"
  grep -q '^SocketGroup=veil$' "$unitdir/veil-helper.socket" || fail "socket group"
  grep -q '^SocketMode=0660$' "$unitdir/veil-helper.socket" || fail "socket mode"
  grep -q '^DirectoryMode=0711$' "$unitdir/veil-helper.socket" || fail "socket dir mode"
  grep -q '^RemoveOnStop=true$' "$unitdir/veil-helper.socket" || fail "socket remove-on-stop"
  if grep -Eq '^ReadWritePaths=.*[[:space:]](/run|/var/run)([[:space:]]|$)' "$unitdir/veil-helper.service"; then
    fail "veil-helper.service still writes to /run"
  fi
}

assert_runtime_readability() {
  # Runtime readability matrix (audit #466): veil-proxy reads the shared
  # material but never panel-only secrets or /var/lib/veil state.
  su -s /bin/sh veil-proxy -c "test -r /etc/veil/panel/tls.key && test -r /etc/veil/www/index.html"     || fail "veil-proxy cannot read shared material"
  if su -s /bin/sh veil-proxy -c "test -r /etc/veil/state.key"; then
    fail "veil-proxy can read panel-only state.key"
  fi
  if su -s /bin/sh veil-proxy -c "test -r /etc/veil/veil.env"; then
    fail "veil-proxy can read panel-only veil.env"
  fi
  if su -s /bin/sh veil-proxy -c "test -r /var/lib/veil/state.json"; then
    fail "veil-proxy can read panel state.json"
  fi
  su -s /bin/sh veil -c "test -r /etc/veil/state.key && test -r /var/lib/veil/state.json && test -r /etc/veil/panel/tls.key"     || fail "veil cannot read its own state/shared material"
}

assert_legacy_www_migrated() {
  # Fallback site moved /var/lib/veil/www -> /etc/veil/www; regular content is
  # carried over, the planted symlink must not be (audit #525). A symlink the
  # operator placed in the destination must survive the upgrade — the
  # migration copies, it never sweeps the destination (issue #622).
  [ "$(cat /etc/veil/www/index.html)" = legacy-index ] || fail "legacy index not migrated"
  [ "$(cat /etc/veil/www/assets/site.css)" = legacy-css ] || fail "legacy asset not migrated"
  { [ ! -L /etc/veil/www/leak.key ] && [ ! -e /etc/veil/www/leak.key ]; }     || fail "legacy symlink leaked into /etc/veil/www"
  [ -L /etc/veil/www/operator.link ] || fail "operator destination symlink swept by upgrade"
  [ -d /var/lib/veil/www ] || fail "legacy /var/lib/veil/www removed on upgrade"
  [ "$(stat -c "%U:%G %a" /etc/veil/www/index.html)" = "root:veil-proxy 640" ]     || fail "migrated index owner/mode"
}

assert_migration_backups() {
  for file in state.json sessions.json state.key veil.env; do
    find /var/lib/veil/migration-backups -type f -name "$file" 2>/dev/null | grep -q . \
      || fail "no migration-backup copy of $file"
  done
}

assert_backup_member_modes() {
  # postinstall re-owns backup trees to root:root and normalizes DIR modes to
  # 0700, but member file modes encode the restore mode and must survive
  # untouched (issue #775) — a find -type f chmod would silently break
  # rollback restores.
  [ "$(stat -c "%U:%G %a" /var/lib/veil/backups)" = "root:root 700" ] \
    || fail "/var/lib/veil/backups owner/mode"
  [ "$(stat -c "%U:%G %a" /var/lib/veil/backups/daily)" = "root:root 700" ] \
    || fail "backup subdir owner/mode"
  [ "$(stat -c "%U:%G %a" /var/lib/veil/backups/daily/state.json.bak)" = "root:root 644" ] \
    || fail "backup member mode was normalized (rollback would restore the wrong mode)"
}

case "$phase" in
  setup-systemd-stub)
    write_stub 0
    mkdir -p /run/systemd/system
    ;;
  setup-systemd-stub-fail)
    write_stub 1
    ;;
  plant-legacy-caddy)
    # Issue #375: a pre-consolidation per-inbound Caddy unit file (vendor dir)
    # plus its enablement wants link must be removed by preremove/postremove.
    mkdir -p /etc/systemd/system/multi-user.target.wants
    printf '[Service]\nExecStart=/usr/local/bin/caddy\n' > /lib/systemd/system/veil-caddy@legacy.service
    ln -sf /lib/systemd/system/veil-caddy@legacy.service \
      /etc/systemd/system/multi-user.target.wants/veil-caddy@legacy.service
    ;;
  write-fixtures)
    printf state-before-upgrade > /var/lib/veil/state.json
    printf sessions-before-upgrade > /var/lib/veil/sessions.json
    printf key-before-upgrade > /etc/veil/state.key
    printf env-before-upgrade > /etc/veil/veil.env
    mkdir -p /etc/veil/panel
    printf panel-tls-key > /etc/veil/panel/tls.key
    chmod 0644 /var/lib/veil/state.json /var/lib/veil/sessions.json /etc/veil/state.key /etc/veil/veil.env
    chmod 0700 /etc/veil/panel
    chmod 0600 /etc/veil/panel/tls.key
    # Legacy layout: fallback site under /var/lib/veil/www (including a
    # symlink that must never be copied into the veil-proxy-readable tree)
    # plus veil-owned caddy/mita state dirs left by the pre-veil-proxy units
    # (issues #497/#623).
    mkdir -p /var/lib/veil/www/assets
    printf legacy-index > /var/lib/veil/www/index.html
    printf legacy-css > /var/lib/veil/www/assets/site.css
    ln -s /etc/veil/panel/tls.key /var/lib/veil/www/leak.key
    # An operator-created symlink already in the destination must survive the
    # upgrade migration — it is not the copy's to delete (issue #622).
    mkdir -p /etc/veil/www
    ln -s /etc/veil/panel/tls.key /etc/veil/www/operator.link
    mkdir -p /var/lib/caddy /var/lib/mita
    chown -R veil:veil /var/lib/veil/www /var/lib/caddy /var/lib/mita
    chmod -R a+r /var/lib/veil/www
    # Backup members encode their restore mode in their own permission bits —
    # postinstall re-owns the tree to root:root but must never normalize
    # member modes to 0600, or rollback restores the wrong mode (issue #775).
    mkdir -p /var/lib/veil/backups/daily
    printf backup-blob > /var/lib/veil/backups/daily/state.json.bak
    chmod 0644 /var/lib/veil/backups/daily/state.json.bak
    chmod 0755 /var/lib/veil/backups/daily
    chown -R veil:veil /var/lib/veil/backups
    ;;
  post-install)
    assert_payload_present
    assert_accounts
    assert_unit_hardening
    assert_version "${2:?expected binary version required}"
    assert_install_systemctl
    ;;
  post-upgrade)
    assert_payload_present
    assert_accounts
    assert_version "${2:?expected binary version required}"
    assert_state_sentinels
    assert_permissions
    assert_unit_hardening
    assert_runtime_readability
    assert_legacy_www_migrated
    assert_migration_backups
    assert_backup_member_modes
    assert_upgrade_systemctl
    ;;
  post-remove|post-purge)
    # Remove AND purge must both leave no packaged payload behind — the units
    # and sysctl drop-in are package-owned, not conffiles (issues #476, #495,
    # #505). Operator state under /etc/veil + /var/lib/veil is NOT
    # package-managed and must survive. The preremove stop/disable contract
    # is asserted via the stub log too (issue #683) — on purge the log still
    # carries the remove leg's entries, so the check holds for both phases.
    assert_payload_absent
    assert_remove_systemctl
    [ "$(cat /var/lib/veil/state.json)" = state-before-upgrade ] || fail "state.json lost on $phase"
    [ "$(cat /etc/veil/state.key)" = key-before-upgrade ] || fail "state.key lost on $phase"
    ;;
  *)
    fail "unknown phase"
    ;;
esac
ASSERTS

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
if [ -n "${prebuilt_dir}" ]; then
  # Release mode: smoke the exact artifacts the release job built instead of a
  # throwaway rebuild (issue #396).
  cp "${prebuilt_dir}"/*.deb "${prebuilt_dir}"/*.rpm "${prebuilt_dir}"/*.apk "${root}/new/"
else
  build_packages "${version_new}" "${root}/new"
fi

repo="$(pwd)"

run_deb_smoke() {
  docker run --rm \
    -e EXPECTED_BINARY_VERSION="${expected_binary_version}" \
    -e OLD_BINARY_VERSION="${binary_version}" \
    -v "${repo}/${root}:/packages:ro" \
    "${CI_SMOKE_DEBIAN_IMAGE}" sh -euxc '
      apt-get update
      apt-get install -y ca-certificates
      sh /packages/smoke-asserts.sh setup-systemd-stub
      dpkg -i /packages/old/*.deb
      sh /packages/smoke-asserts.sh post-install "$OLD_BINARY_VERSION"

      sh /packages/smoke-asserts.sh write-fixtures
      systemctl enable veil.service veil-helper.socket
      : > /tmp/systemctl.log
      dpkg -i /packages/new/*.deb
      sh /packages/smoke-asserts.sh post-upgrade "$EXPECTED_BINARY_VERSION"

      : > /tmp/systemctl.log
      sh /packages/smoke-asserts.sh plant-legacy-caddy
      dpkg -r veil
      sh /packages/smoke-asserts.sh post-remove
      # Purge exercises the conffile/cleanup path too (issue #495).
      dpkg -P veil
      sh /packages/smoke-asserts.sh post-purge

      : > /tmp/systemctl.log
      dpkg -i /packages/new/*.deb
      sh /packages/smoke-asserts.sh post-install "$EXPECTED_BINARY_VERSION"
    '
}

run_rpm_smoke() {
  docker run --rm \
    -e EXPECTED_BINARY_VERSION="${expected_binary_version}" \
    -e OLD_BINARY_VERSION="${binary_version}" \
    -v "${repo}/${root}:/packages:ro" \
    "${CI_SMOKE_ROCKYLINUX_IMAGE}" sh -euxc '
      sh /packages/smoke-asserts.sh setup-systemd-stub
      dnf install -y /packages/old/*.rpm
      sh /packages/smoke-asserts.sh post-install "$OLD_BINARY_VERSION"

      sh /packages/smoke-asserts.sh write-fixtures
      systemctl enable veil.service veil-helper.socket
      : > /tmp/systemctl.log
      dnf upgrade -y /packages/new/*.rpm
      sh /packages/smoke-asserts.sh post-upgrade "$EXPECTED_BINARY_VERSION"

      sh /packages/smoke-asserts.sh plant-legacy-caddy
      dnf remove -y veil
      sh /packages/smoke-asserts.sh post-remove

      : > /tmp/systemctl.log
      dnf install -y /packages/new/*.rpm
      sh /packages/smoke-asserts.sh post-install "$EXPECTED_BINARY_VERSION"
    '
}

run_apk_smoke() {
  docker run --rm \
    -e EXPECTED_BINARY_VERSION="${expected_binary_version}" \
    -e OLD_BINARY_VERSION="${binary_version}" \
    -v "${repo}/${root}:/packages:ro" \
    "${CI_SMOKE_ALPINE_IMAGE}" sh -euxc '
      sh /packages/smoke-asserts.sh setup-systemd-stub
      apk add --allow-untrusted /packages/old/*.apk
      sh /packages/smoke-asserts.sh post-install "$OLD_BINARY_VERSION"

      sh /packages/smoke-asserts.sh write-fixtures
      systemctl enable veil.service veil-helper.socket
      : > /tmp/systemctl.log
      apk add --allow-untrusted --upgrade /packages/new/*.apk
      sh /packages/smoke-asserts.sh post-upgrade "$EXPECTED_BINARY_VERSION"

      sh /packages/smoke-asserts.sh plant-legacy-caddy
      apk del veil
      sh /packages/smoke-asserts.sh post-remove

      : > /tmp/systemctl.log
      apk add --allow-untrusted /packages/new/*.apk
      sh /packages/smoke-asserts.sh post-install "$EXPECTED_BINARY_VERSION"
    '
}

# Symlink-refusal + real installer contract leg. Intentionally NO systemctl
# stub and NO /run/systemd/system marker: this is the honest non-systemd
# container where `veil install --check` must refuse, and postinstall must
# skip systemd silently instead of failing (image-build compatibility).
run_symlink_refusal_smoke() {
  docker run --rm \
    -v "${repo}/${root}:/packages:ro" \
    "${CI_SMOKE_DEBIAN_IMAGE}" sh -euxc '
      apt-get update
      apt-get install -y ca-certificates
      dpkg -i /packages/old/*.deb

      # Real installer contract of the packaged binary (audit #304): on this
      # non-systemd container `veil install --check` must print the capability
      # report and refuse with an actionable init failure instead of mutating.
      if /usr/local/bin/veil install --check >/tmp/install-check.out 2>&1; then
        echo "veil install --check unexpectedly succeeded without systemd" >&2
        exit 1
      fi
      grep -q "capability report" /tmp/install-check.out
      grep -q "init" /tmp/install-check.out
      test ! -e /var/lib/veil/state.json


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

# Fail-closed leg (issues #477/#498/#504): with systemd marked as running, a
# failing systemctl must abort postinstall — and without the marker the same
# failing systemctl must never be invoked at all.
run_fail_closed_smoke() {
  docker run --rm \
    -v "${repo}/${root}:/packages:ro" \
    "${CI_SMOKE_DEBIAN_IMAGE}" sh -euxc '
      apt-get update
      apt-get install -y ca-certificates
      sh /packages/smoke-asserts.sh setup-systemd-stub-fail

      # Container/image-build path: systemd is not init, postinstall must not
      # call systemctl at all — a permanently failing stub proves the gate.
      dpkg -i /packages/old/*.deb
      if [ -s /tmp/systemctl.log ]; then
        echo "postinstall invoked systemctl without a running systemd" >&2
        cat /tmp/systemctl.log >&2
        exit 1
      fi

      # Live-systemd path: daemon-reload failure must fail the install.
      mkdir -p /run/systemd/system
      dpkg -r veil
      : > /tmp/systemctl.log
      if dpkg -i /packages/old/*.deb; then
        echo "postinstall succeeded despite failing systemctl daemon-reload" >&2
        exit 1
      fi
      grep -q "systemctl daemon-reload" /tmp/systemctl.log
    '
}

run_deb_smoke
run_rpm_smoke
run_apk_smoke
run_symlink_refusal_smoke
run_fail_closed_smoke

echo 'Native package install, upgrade, purge, reinstall, permissions, migration safety, and fail-closed smoke tests passed.'
