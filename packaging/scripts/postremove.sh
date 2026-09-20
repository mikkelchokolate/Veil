#!/bin/sh
# Veil package postremove: reload systemd after unit files are removed.
# State under /etc/veil is intentionally preserved so reinstalling Veil keeps
# Panel credentials and generated configs. Use `veil uninstall` for full removal.
set -e

# Debian postrm and RPM %postun also run on upgrade. Unit/sysctl cleanup would
# delete the NEW package's files then, so only run it on a real remove/purge
# (same argument contract as preremove.sh).
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

# Do not assume the package manager already removed the packaged systemd units
# and the QUIC sysctl drop-in: packages built before issue #475 ship them as
# conffiles that survive `dpkg -r`/`apt remove` until purge, and a partially
# failed remove can leave strays too. Package-owned vendor paths are removed
# explicitly here. Operator units under /etc/systemd/system are NOT touched —
# they belong to `veil install`, and `veil uninstall` owns their lifecycle.
for dir in /lib/systemd/system /usr/lib/systemd/system; do
    [ -d "$dir" ] || continue
    for unit in "$dir"/veil.service "$dir"/veil-helper.service "$dir"/veil-helper.socket \
        "$dir"/veil-backup.service "$dir"/veil-backup.timer "$dir"/veil-caddy.service \
        "$dir"/veil-hysteria2@.service "$dir"/veil-mieru.service "$dir"/veil-olcrtc@.service \
        "$dir"/veil-warp.service; do
        [ -f "$unit" ] || continue
        rm -f "$unit"
    done
done
rm -f /etc/sysctl.d/99-veil-quic.conf

if command -v systemctl >/dev/null 2>&1; then
    # Removal must never fail because systemd is not running (containers,
    # chroots); the reload is best-effort on purpose here.
    systemctl daemon-reload >/dev/null 2>&1 || true
fi
