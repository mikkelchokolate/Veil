#!/bin/sh
# Veil package preremove: stop and disable managed units before files are removed.
# Debian prerm and RPM %preun also run on upgrade. Skip stop/disable then so a
# live install is not disabled across apt/dnf upgrade.
set -e

is_upgrade() {
    arg="${1:-}"
    case "$arg" in
        upgrade|deconfigure|failed-upgrade)
            return 0
            ;;
        remove|purge|0|"")
            return 1
            ;;
    esac
    # RPM leftover count: a positive integer means another instance remains.
    case "$arg" in
        *[!0-9]*) return 1 ;;
    esac
    [ "$arg" -gt 0 ]
}

if is_upgrade "${1:-}"; then
    exit 0
fi

stop_disable_unit() {
    unit="$1"
    systemctl stop "$unit" >/dev/null 2>&1 || true
    systemctl disable "$unit" >/dev/null 2>&1 || true
}

stop_disable_matching_units() {
    pattern="$1"
    units="$(
        {
            systemctl list-units --all --plain --no-legend "$pattern" 2>/dev/null || true
            systemctl list-unit-files --plain --no-legend "$pattern" 2>/dev/null || true
        } | awk '{ print $1 }'
    )"
    for unit in $units; do
        case "$unit" in
            *.service|*.socket|*.timer) stop_disable_unit "$unit" ;;
        esac
    done
}

if command -v systemctl >/dev/null 2>&1; then
    # The timer does not imply the service: a running/on-enabled
    # veil-backup.service must be stopped and disabled like every other unit
    # (issue #480).
    stop_disable_unit veil-backup.timer
    stop_disable_unit veil-backup.service
    stop_disable_unit veil.service
    stop_disable_unit veil-helper.service
    stop_disable_unit veil-helper.socket
    stop_disable_unit veil-mieru.service
    stop_disable_unit veil-warp.service

    stop_disable_unit veil-caddy.service
    stop_disable_matching_units 'veil-hysteria2@*.service'
    stop_disable_matching_units 'veil-olcrtc@*.service'
    # Legacy pre-consolidation per-inbound Caddy instances are not in the
    # current unit catalog (apply cleans them via the orphan prefix scan);
    # package removal must stop/disable them the same way (issue #375).
    stop_disable_matching_units 'veil-caddy@*.service'
fi

# A dangling multi-user.target.wants link survives `systemctl disable` when the
# instance's unit file is already gone — and with no running systemd the
# disable above never ran at all. Sweep legacy caddy wants links explicitly so
# package removal cannot leave them enabled (issue #375).
for wants_dir in /etc/systemd/system/multi-user.target.wants \
    /lib/systemd/system/multi-user.target.wants \
    /usr/lib/systemd/system/multi-user.target.wants; do
    [ -d "$wants_dir" ] || continue
    for link in "$wants_dir"/veil-caddy@*.service; do
        if [ -e "$link" ] || [ -L "$link" ]; then
            rm -f "$link"
        fi
    done
done
