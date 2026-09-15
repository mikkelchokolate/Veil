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
    stop_disable_unit veil-backup.timer
    stop_disable_unit veil.service
    stop_disable_unit veil-helper.service
    stop_disable_unit veil-helper.socket
    stop_disable_unit veil-mieru.service
    stop_disable_unit veil-warp.service

    stop_disable_unit veil-caddy.service
    stop_disable_matching_units 'veil-hysteria2@*.service'
    stop_disable_matching_units 'veil-olcrtc@*.service'
fi
