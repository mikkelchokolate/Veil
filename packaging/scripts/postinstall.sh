#!/bin/sh
# Veil package postinstall: reload systemd so the managed units are visible.
# Veil install (panel access, credentials, Caddy/TLS) is run separately by the
# operator via `veil install`; packages only deliver the binary and units.
set -e

# Debian does not only run postinst as `configure [old-version]`: dpkg also
# calls it as abort-upgrade <new-version>, abort-remove, abort-deconfigure and
# triggered <names> when it rolls an operation back or processes triggers
# (deb-postinst(5)). Those paths exist to restore what the rolled-back version
# left behind, so the install body below — account setup, `veil helper
# migrate` (which writes a fresh migration-backups snapshot holding copies of
# state.key/state.json) and a try-restart of every managed unit — must not run
# there. RPM passes only the install counter ($1: 1 install, 2 upgrade) and APK
# passes nothing, so both keep the install body. abort-remove is the one abort
# that has work to do: preremove stopped the units, so restart the ones that
# are still installed without touching host state (#1463).
postinstall_mode() {
    arg="${1:-}"
    case "$arg" in
        configure)
            echo install
            return 0
            ;;
        "")
            # Plain invocation: APK's post-install hook and the historic
            # no-argument call path.
            echo install
            return 0
            ;;
        abort-remove)
            echo restart
            return 0
            ;;
        abort-upgrade|abort-deconfigure|triggered)
            echo skip
            return 0
            ;;
    esac
    # RPM install counter: 1 = install, 2 = upgrade. Anything non-numeric is an
    # argument we do not know, so fail closed and leave host state alone.
    case "$arg" in
        *[!0-9]*) echo skip; return 0 ;;
    esac
    if [ "$arg" -gt 0 ]; then
        echo install
    else
        echo skip
    fi
}

MODE="$(postinstall_mode "${1:-}")"
if [ "$MODE" = skip ]; then
    exit 0
fi

group_exists() {
    name="$1"
    if command -v getent >/dev/null 2>&1; then
        getent group "$name" >/dev/null 2>&1
    else
        grep -q "^${name}:" /etc/group
    fi
}

ensure_system_account() {
    name="$1"
    if ! group_exists "$name"; then
        if command -v groupadd >/dev/null 2>&1; then
            groupadd --system "$name"
        elif command -v addgroup >/dev/null 2>&1; then
            addgroup -S "$name"
        else
            echo "Cannot create group $name: neither groupadd (shadow-utils) nor addgroup is installed" >&2
            exit 1
        fi
    fi
    if ! id -u "$name" >/dev/null 2>&1; then
        nologin=/usr/sbin/nologin
        [ -x "$nologin" ] || nologin=/sbin/nologin
        [ -x "$nologin" ] || nologin=/bin/false
        if command -v useradd >/dev/null 2>&1; then
            useradd --system --gid "$name" --no-create-home --home-dir /nonexistent --shell "$nologin" "$name"
        elif command -v adduser >/dev/null 2>&1; then
            adduser -S -D -H -h /nonexistent -s "$nologin" -G "$name" "$name"
        else
            echo "Cannot create user $name: neither useradd (shadow-utils) nor adduser is installed" >&2
            exit 1
        fi
    fi
}

if [ "$MODE" = install ]; then
    ensure_system_account veil
    ensure_system_account veil-proxy
    # veil-mita is the dedicated veil-mieru.service identity (issue #624): its
    # appctl socket is a control plane, so it must not share the veil-proxy edge
    # uid/group that every other internet-facing unit uses.
    ensure_system_account veil-mita

    # A silent membership failure would leave the managed directories unreadable
    # (veil-proxy) or the mita appctl socket unreachable (veil-mita) after the
    # ownership pass below, so stop loudly.
    ensure_group_member() {
        member_user="$1"
        member_group="$2"
        if ! id -nG "$member_user" 2>/dev/null | tr ' ' '\n' | grep -qx "$member_group"; then
            if command -v usermod >/dev/null 2>&1; then
                usermod -aG "$member_group" "$member_user"
            elif command -v addgroup >/dev/null 2>&1; then
                addgroup "$member_user" "$member_group"
            else
                echo "Cannot add user $member_user to group $member_group: neither usermod nor addgroup is installed" >&2
                exit 1
            fi
        fi
    }
    # The protocol units and veil-caddy.service run as veil-proxy and read
    # /etc/veil/generated, /etc/veil/tls, /etc/veil/panel, and /etc/veil/certs
    # directly. The panel account is a supplementary veil-proxy member so it can
    # keep previewing that material.
    ensure_group_member veil veil-proxy
    # The panel is also the ONLY appctl client: it joins veil-mita so
    # `mita get metrics` can connect to /run/veil-mieru/mita.sock (0770
    # veil-mita). veil-proxy units are deliberately NOT members.
    ensure_group_member veil veil-mita

    if [ -f /etc/sysctl.d/99-veil-quic.conf ]; then
        # Live sysctl can be rejected on OpenVZ/LXC-style kernels where /proc/sys
        # is read-only. Persist-or-warn like `veil install` does: the drop-in stays
        # on disk for the next boot, but the failure is recorded loudly instead of
        # being silently discarded (issue #477).
        if ! sysctl -p /etc/sysctl.d/99-veil-quic.conf >/dev/null 2>&1; then
            echo "warning: sysctl -p /etc/sysctl.d/99-veil-quic.conf failed; QUIC UDP buffer limits were not applied to the running kernel" >&2
        fi
fi

if [ -d /lib/systemd/system ] && [ -d /etc/systemd/system ]; then
    for unit in /lib/systemd/system/veil*.service /lib/systemd/system/veil*.socket /lib/systemd/system/veil*.timer; do
        [ -f "$unit" ] || continue
        name=$(basename "$unit")
        etc="/etc/systemd/system/$name"
        [ -f "$etc" ] || continue
        # Only drop /etc units Veil itself generated (marker line) or byte-
        # identical stale copies of the packaged unit. An operator-authored
        # override that merely shares the ExecStart line is NOT removed
        # (issue #488).
        if grep -q '^# Generated by veil install\.' "$etc" 2>/dev/null || cmp -s "$unit" "$etc"; then
            rm -f "$etc"
        fi
    done
fi

# Ownership/permission normalization over /etc/veil, /var/lib/veil,
# /var/lib/caddy and /var/lib/mita is delegated to the installed binary, which
# runs the descriptor-pinned hostaccess.Prepare -> Migrate pass. These trees
# are writable by the veil* service accounts — including the daemons still
# running while this upgrade executes — so a shell `find -exec chmod` /
# `chown -R` walk would race a leaf or directory swapped for a symlink: find
# decides -type from an lstat and the mode change then follows the path onto
# an arbitrary root-owned target (e.g. /usr/bin/sudo), and `install -d` /
# `chown -R` follow a swapped top-level component the same way (#1143).
# hostaccess opens each directory O_NOFOLLOW|O_DIRECTORY relative to its
# pinned parent and applies fchmod/fchown on held descriptors, so a swapped
# entry fails the pass instead of being followed. The single invocation covers
# everything the old hand-rolled blocks did: /etc/veil (0751 root:veil) and
# /var/lib/veil (0750 veil:veil); the safety snapshot into
# /var/lib/veil/migration-backups; /var/lib/veil/{audit,staging,updates,
# autocert} at veil:veil with dirs 0700 and files 0600; the root-owned
# backups/promotion-backups/migration-backups trees (directories 0700 only —
# backup member modes carry restore semantics and are never flattened);
# /var/lib/veil/{state.json,sessions.json,sessions.json.journal,
# sessions.json.lock} at veil:veil 0600; the legacy
# /var/lib/veil/www carry-over into /etc/veil/www (regular files and real
# directories only, never symlinks); the runtime-shared subtrees
# /etc/veil/{generated,tls,certs,www} and /etc/veil/panel at root:veil-proxy
# 0750/0640 so the veil-proxy units and the panel (supplementary member) can
# read them;
# /etc/veil/{state.key,veil.env} at root:veil 0640 and backup.passphrase at
# root:root 0600; and the /var/lib/caddy (veil-proxy) plus /var/lib/mita
# (veil-mita) StateDirectory re-owns.
/usr/local/bin/veil helper migrate

# The helper socket parent must be root-owned and traverse-only. Older units
# created it via the panel's RuntimeDirectory=veil (veil:veil 0750), which let
# any veil-uid process replace the helper socket; normalize on upgrade.
install -d -m 0711 -o root -g root /run/veil
fi

# Only drive systemd when it is the running init. Containers building images
# or chroots may ship a systemctl binary with no live systemd behind it —
# `dpkg -i` must still succeed there (issue #498). When systemd IS running,
# every call below fails closed: a daemon-reload/enable/restart failure is a
# real packaging failure, not something to silence (issues #498, #504).
restart_managed_units() {
    if ! command -v systemctl >/dev/null 2>&1 || [ ! -d /run/systemd/system ]; then
        return 0
    fi
    systemctl daemon-reload
    # Restart already-running units so they pick up the replaced binary. This is
    # a no-op on a fresh package install (units are not running yet).
    systemctl try-restart veil.service veil-helper.service veil-helper.socket veil-caddy.service veil-mieru.service veil-warp.service veil-backup.service veil-backup.timer
    # Template units restart per concrete instance — a `@*` glob can miss
    # loaded instances, so enumerate them explicitly (issue #494).
    for instance in $(systemctl list-units --all --plain --no-legend 'veil-hysteria2@*.service' 'veil-olcrtc@*.service' 2>/dev/null | awk '{ print $1 }'); do
        systemctl try-restart "$instance"
    done
}

# Enabling the helper socket is install-only: abort-remove must not re-enable a
# socket the operator disabled before the aborted removal.
if [ "$MODE" = install ] && command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    systemctl enable veil-helper.socket
fi

# install: units pick up the replaced binary. restart (abort-remove): bring
# back what preremove stopped, nothing else.
if [ "$MODE" = install ] || [ "$MODE" = restart ]; then
    restart_managed_units
fi

if [ "$MODE" = install ]; then
    echo "Veil installed. Run 'veil install' to configure Panel access, or"
    echo "'veil doctor' to check host readiness."
fi
