#!/bin/sh
# Veil package postinstall: reload systemd so the managed units are visible.
# Veil install (panel access, credentials, Caddy/TLS) is run separately by the
# operator via `veil install`; packages only deliver the binary and units.
set -e

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

install -d -m 0751 -o root -g veil /etc/veil
install -d -m 0750 -o veil -g veil /var/lib/veil

safety_sources="/etc/veil/state.key /etc/veil/veil.env /var/lib/veil/state.json /var/lib/veil/sessions.json"
has_safety_source=false
for source in $safety_sources; do
    if [ -e "$source" ]; then
        if [ -L "$source" ] || [ ! -f "$source" ]; then
            echo "Refusing to migrate non-regular managed file: $source" >&2
            exit 1
        fi
        has_safety_source=true
    fi
done
if [ "$has_safety_source" = true ]; then
    stamp=$(date -u +%Y%m%dT%H%M%SZ)
    safety_dir="/var/lib/veil/migration-backups/$stamp"
    suffix=0
    while [ -e "$safety_dir" ]; do
        suffix=$((suffix + 1))
        safety_dir="/var/lib/veil/migration-backups/$stamp-$suffix"
    done
    install -d -m 0700 -o root -g root "$safety_dir"
    for source in $safety_sources; do
        if [ -f "$source" ]; then
            install -m 0600 -o root -g root "$source" "$safety_dir/$(basename "$source")"
        fi
    done
fi

for dir in audit staging updates autocert; do
    install -d -m 0700 -o veil -g veil "/var/lib/veil/$dir"
    chown -R veil:veil "/var/lib/veil/$dir"
    find "/var/lib/veil/$dir" -type d -exec chmod 0700 {} \;
    find "/var/lib/veil/$dir" -type f -exec chmod 0600 {} \;
done
# /var/lib/veil/www is no longer provisioned: veil-caddy.service runs as
# veil-proxy with /var/lib/veil in InaccessiblePaths, so the old fallback root
# is unreachable. Existing content is migrated to /etc/veil/www below.
for dir in backups promotion-backups migration-backups; do
    install -d -m 0700 -o root -g root "/var/lib/veil/$dir"
    chown -R root:root "/var/lib/veil/$dir"
    find "/var/lib/veil/$dir" -type d -exec chmod 0700 {} \;
    # Backup members store restore mode in their own permission bits. Do not
    # normalize files to 0600 or rollback will restore the wrong mode.
done
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
for file in /var/lib/veil/state.json /var/lib/veil/sessions.json; do
    if [ -f "$file" ] && [ ! -L "$file" ]; then
        chown veil:veil "$file"
        chmod 0600 "$file"
    fi
done
# The naive fallback site moved from /var/lib/veil/www to /etc/veil/www:
# veil-caddy.service runs as veil-proxy with /var/lib/veil in
# InaccessiblePaths, so the old location is unreadable to it. Carry over any
# operator content not already present in the new root — regular files and
# directories only; symlinks are never copied into a veil-proxy-readable tree.
if [ -d /var/lib/veil/www ] && [ ! -L /var/lib/veil/www ]; then
    install -d -m 0750 -o root -g veil-proxy /etc/veil/www
    for item in /var/lib/veil/www/* /var/lib/veil/www/.[!.]* /var/lib/veil/www/..?*; do
        [ -e "$item" ] || continue
        [ -L "$item" ] && continue
        base=${item##*/}
        if [ ! -e "/etc/veil/www/$base" ]; then
            cp -Rp "$item" "/etc/veil/www/$base"
        fi
    done
    find /etc/veil/www -type l -delete
fi
for dir in /etc/veil/generated /etc/veil/tls /etc/veil/certs /etc/veil/www /etc/veil/panel; do
    if [ -L "$dir" ]; then
        echo "Refusing to repair symlinked managed directory: $dir" >&2
        exit 1
    fi
    # Runtime-shared material is root:veil-proxy 0750/0640 so the protocol
    # units (User=veil-proxy) and the panel (supplementary veil-proxy member)
    # can read generated config, TLS keys, and ACME output (audit #525/#531).
    install -d -m 0750 -o root -g veil-proxy "$dir"
    chown -R root:veil-proxy "$dir"
    find "$dir" -type d -exec chmod 0750 {} \;
    find "$dir" -type f -exec chmod 0640 {} \;
done
for file in /etc/veil/state.key /etc/veil/veil.env; do
    if [ -f "$file" ] && [ ! -L "$file" ]; then
        chown root:veil "$file"
        chmod 0640 "$file"
    fi
done
if [ -f /etc/veil/backup.passphrase ] && [ ! -L /etc/veil/backup.passphrase ]; then
    chown root:root /etc/veil/backup.passphrase
    chmod 0600 /etc/veil/backup.passphrase
fi

# The helper socket parent must be root-owned and traverse-only. Older units
# created it via the panel's RuntimeDirectory=veil (veil:veil 0750), which let
# any veil-uid process replace the helper socket; normalize on upgrade.
install -d -m 0711 -o root -g root /run/veil

# veil-caddy.service switched from User=veil to User=veil-proxy (#497).
# systemd does not re-own an existing StateDirectory, so an ACME data dir
# left at veil:veil would be unwritable for the new account — re-own it.
if [ -d /var/lib/caddy ] && [ ! -L /var/lib/caddy ]; then
    chown -R veil-proxy:veil-proxy /var/lib/caddy
fi

# veil-mieru.service switched from User=veil-proxy to the dedicated veil-mita
# identity (#624): a mita StateDirectory left at veil-proxy:veil-proxy would
# be unwritable for the daemon — same re-own as caddy above. Match the
# hostaccess.Migrate mode contract too (dirs 0700, files 0600): find does not
# follow symlinks, so a planted link is skipped rather than tightened.
if [ -d /var/lib/mita ] && [ ! -L /var/lib/mita ]; then
    chown -R veil-mita:veil-mita /var/lib/mita
    find /var/lib/mita -type d -exec chmod 0700 {} +
    find /var/lib/mita -type f -exec chmod 0600 {} +
fi

# Only drive systemd when it is the running init. Containers building images
# or chroots may ship a systemctl binary with no live systemd behind it —
# `dpkg -i` must still succeed there (issue #498). When systemd IS running,
# every call below fails closed: a daemon-reload/enable/restart failure is a
# real packaging failure, not something to silence (issues #498, #504).
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    systemctl daemon-reload
    systemctl enable veil-helper.socket
    # Restart already-running units so they pick up the replaced binary. This is
    # a no-op on a fresh package install (units are not running yet).
    systemctl try-restart veil.service veil-helper.service veil-helper.socket veil-caddy.service veil-mieru.service veil-warp.service veil-backup.service veil-backup.timer
    # Template units restart per concrete instance — a `@*` glob can miss
    # loaded instances, so enumerate them explicitly (issue #494).
    for instance in $(systemctl list-units --all --plain --no-legend 'veil-hysteria2@*.service' 'veil-olcrtc@*.service' 2>/dev/null | awk '{ print $1 }'); do
        systemctl try-restart "$instance"
    done
fi

echo "Veil installed. Run 'veil install' to configure Panel access, or"
echo "'veil doctor' to check host readiness."
