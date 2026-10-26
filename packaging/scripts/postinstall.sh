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
    # RPM passes its install counter as $1 ("1" install, "2" upgrade); APK
    # passes the package version, with the old version as $2 on upgrade
    # (apk-tools run_script). Both mean the package's files were just unpacked,
    # so a leading digit plus a version charset is the install path. Anything
    # else is an argument we do not know: fail closed and leave host state
    # alone.
    case "$arg" in
        [0-9]*)
            case "$arg" in
                *[!0-9A-Za-z._+~-]*) echo skip; return 0 ;;
            esac
            if [ "$arg" = 0 ]; then
                echo skip
            else
                echo install
            fi
            return 0
            ;;
    esac
    echo skip
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
        # Only raise: a host already tuned above Veil's 16 MiB floor keeps its
        # limits, and the drop-in is rewritten to that observed value so the
        # next boot does not lower them either (issue #1456).
        #
        # The live value alone is not enough: sysctl.d files apply in
        # lexicographic order and Veil's own 99-veil-quic.conf sorts after an
        # operator's 90-network-tuning.conf, so /proc already reads Veil's clamp
        # and the tuned value would be lost on every upgrade (issue #1492). Read
        # the explicit assignments too and keep the highest.
        quic_explicit_max() {
            awk -v key="$1" '
                { sub(/^[[:space:]]+/, "")
                  if ($0 == "" || $0 ~ /^[#;]/) next
                  sub(/^-/, "")
                  eq = index($0, "=")
                  if (eq == 0) next
                  name = substr($0, 1, eq - 1)
                  gsub(/[[:space:]]/, "", name)
                  if (name != key) next
                  value = substr($0, eq + 1)
                  gsub(/[[:space:]]/, "", value)
                  if (value ~ /^[0-9]+$/ && value + 0 > best) best = value + 0 }
                END { print best + 0 }' "$@" 2>/dev/null || true
        }
        quic_floor=16777216
        quic_rmem="$(cat /proc/sys/net/core/rmem_max 2>/dev/null || true)"
        quic_wmem="$(cat /proc/sys/net/core/wmem_max 2>/dev/null || true)"
        case "${quic_rmem}" in '' | *[!0-9]*) quic_rmem="${quic_floor}" ;; esac
        case "${quic_wmem}" in '' | *[!0-9]*) quic_wmem="${quic_floor}" ;; esac
        [ "${quic_rmem}" -ge "${quic_floor}" ] || quic_rmem="${quic_floor}"
        [ "${quic_wmem}" -ge "${quic_floor}" ] || quic_wmem="${quic_floor}"
        quic_tuned_rmem="$(quic_explicit_max net.core.rmem_max \
            /etc/sysctl.d/*.conf /run/sysctl.d/*.conf /usr/local/lib/sysctl.d/*.conf \
            /usr/lib/sysctl.d/*.conf /lib/sysctl.d/*.conf /etc/sysctl.conf)"
        quic_tuned_wmem="$(quic_explicit_max net.core.wmem_max \
            /etc/sysctl.d/*.conf /run/sysctl.d/*.conf /usr/local/lib/sysctl.d/*.conf \
            /usr/lib/sysctl.d/*.conf /lib/sysctl.d/*.conf /etc/sysctl.conf)"
        case "${quic_tuned_rmem}" in '' | *[!0-9]*) quic_tuned_rmem=0 ;; esac
        case "${quic_tuned_wmem}" in '' | *[!0-9]*) quic_tuned_wmem=0 ;; esac
        [ "${quic_rmem}" -ge "${quic_tuned_rmem}" ] || quic_rmem="${quic_tuned_rmem}"
        [ "${quic_wmem}" -ge "${quic_tuned_wmem}" ] || quic_wmem="${quic_tuned_wmem}"
        cat > /etc/sysctl.d/99-veil-quic.conf <<SYSCTL
# Hysteria2/QUIC needs large UDP socket buffers. Linux defaults (~208 KiB)
# drop inbound datagrams and cap client upload around 5-10 Mbit/s while
# download (server send path) can still look fine.
# Values are floored at 16 MiB and never lower an existing higher limit
# (issue #1456).
net.core.rmem_max = ${quic_rmem}
net.core.wmem_max = ${quic_wmem}
SYSCTL
        # Live sysctl can be rejected on OpenVZ/LXC-style kernels where
        # /proc/sys is read-only. Persist-or-warn like `veil install` does: the
        # drop-in stays on disk for the next boot, but the failure is recorded
        # loudly instead of being silently discarded (issue #477).
        if [ "${quic_rmem}" -lt "${quic_floor}" ]; then quic_rmem="${quic_floor}"; fi
        if [ "${quic_wmem}" -lt "${quic_floor}" ]; then quic_wmem="${quic_floor}"; fi
        quic_live_rmem="$(cat /proc/sys/net/core/rmem_max 2>/dev/null || true)"
        quic_live_wmem="$(cat /proc/sys/net/core/wmem_max 2>/dev/null || true)"
        case "${quic_live_rmem}" in '' | *[!0-9]*) quic_live_rmem=0 ;; esac
        case "${quic_live_wmem}" in '' | *[!0-9]*) quic_live_wmem=0 ;; esac
        if [ "${quic_live_rmem}" -lt "${quic_rmem}" ]; then
            if ! sysctl -w "net.core.rmem_max=${quic_rmem}" >/dev/null 2>&1; then
                echo "warning: sysctl -w net.core.rmem_max=${quic_rmem} failed; the QUIC UDP receive limit was not raised on the running kernel" >&2
            fi
        fi
        if [ "${quic_live_wmem}" -lt "${quic_wmem}" ]; then
            if ! sysctl -w "net.core.wmem_max=${quic_wmem}" >/dev/null 2>&1; then
                echo "warning: sysctl -w net.core.wmem_max=${quic_wmem} failed; the QUIC UDP send limit was not raised on the running kernel" >&2
            fi
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
#
# The same unit list is driven by two verbs, and they are not interchangeable:
# configure/upgrade runs while the units are still running, so try-restart
# swaps them onto the replaced binary (a no-op on a fresh install); abort-remove
# runs right after preremove stopped and disabled those units, and systemctl(1)
# defines try-restart as doing nothing to a unit that is not running, so that
# path has to start them.
restart_managed_units() {
    systemctl daemon-reload
    systemctl enable veil-helper.socket
    systemctl try-restart veil.service veil-helper.service veil-helper.socket veil-caddy.service veil-mieru.service veil-warp.service veil-backup.service veil-backup.timer
    # Template units restart per concrete instance — a `@*` glob can miss
    # loaded instances, so enumerate them explicitly (issue #494).
    for instance in $(systemctl list-units --all --plain --no-legend 'veil-hysteria2@*.service' 'veil-olcrtc@*.service' 2>/dev/null | awk '{ print $1 }'); do
        systemctl try-restart "$instance"
    done
}

start_managed_units() {
    systemctl daemon-reload
    # preremove disables every managed unit on the remove path, so an aborted
    # removal has to restore that enable set or the panel, the proxies and the
    # backup timer stay disabled after the next reboot (#1466). The list has to
    # match preremove.sh's stop/disable set; a test runs both scripts and
    # compares the sets. veil-helper.service and veil-backup.service are
    # deliberately absent: neither has an [Install] section (socket-activated
    # and timer-triggered), so systemctl enable refuses them and their
    # enablement means nothing — the socket and the timer are the enabled units.
    for unit in veil.service veil-helper.socket veil-caddy.service veil-mieru.service veil-warp.service veil-backup.timer; do
        systemctl enable "$unit" \
            || echo "warning: could not enable $unit after the aborted removal" >&2
    done
    # Template instances are enabled per concrete instance: a `@*` pattern can
    # miss loaded instances, and legacy pre-consolidation veil-caddy@ units are
    # in preremove's sweep too.
    for pattern in 'veil-hysteria2@*.service' 'veil-olcrtc@*.service' 'veil-caddy@*.service'; do
        for instance in $(systemctl list-unit-files --plain --no-legend "$pattern" 2>/dev/null | awk '{ print $1 }'); do
            systemctl enable "$instance" \
                || echo "warning: could not enable $instance after the aborted removal" >&2
        done
    done
    # Start what the rolled-back removal left stopped, and log instead of
    # failing the package when a unit is not configured on this host: an
    # already-aborted removal must not become a dpkg post-installation-script
    # error (#1465). veil-backup.service is a oneshot: starting it here would
    # run a real backup and prune during the rollback, so only its timer is
    # started.
    for unit in veil.service veil-helper.service veil-helper.socket veil-caddy.service veil-mieru.service veil-warp.service veil-backup.timer; do
        systemctl start "$unit" \
            || echo "warning: could not start $unit after the aborted removal" >&2
    done
    for pattern in 'veil-hysteria2@*.service' 'veil-olcrtc@*.service' 'veil-caddy@*.service'; do
        for instance in $(systemctl list-units --all --plain --no-legend "$pattern" 2>/dev/null | awk '{ print $1 }'); do
            systemctl start "$instance" \
                || echo "warning: could not start $instance after the aborted removal" >&2
        done
    done
}

if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
    case "$MODE" in
        install) restart_managed_units ;;
        restart) start_managed_units ;;
    esac
fi

if [ "$MODE" = install ]; then
    echo "Veil installed. Run 'veil install' to configure Panel access, or"
    echo "'veil doctor' to check host readiness."
fi
