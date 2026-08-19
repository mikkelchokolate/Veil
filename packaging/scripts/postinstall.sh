#!/bin/sh
# Veil package postinstall: reload systemd so the managed units are visible.
# Veil install (panel access, credentials, Caddy/TLS) is run separately by the
# operator via `veil install`; packages only deliver the binary and units.
set -e

group_exists() {
    if command -v getent >/dev/null 2>&1; then
        getent group veil >/dev/null 2>&1
    else
        grep -q '^veil:' /etc/group
    fi
}

if ! group_exists; then
    if command -v groupadd >/dev/null 2>&1; then
        groupadd --system veil
    else
        addgroup -S veil
    fi
fi

if ! id -u veil >/dev/null 2>&1; then
    nologin=/usr/sbin/nologin
    [ -x "$nologin" ] || nologin=/sbin/nologin
    [ -x "$nologin" ] || nologin=/bin/false
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --gid veil --no-create-home --home-dir /nonexistent --shell "$nologin" veil
    else
        adduser -S -D -H -h /nonexistent -s "$nologin" -G veil veil
    fi
fi

if [ -f /etc/sysctl.d/99-veil-quic.conf ]; then
    sysctl -p /etc/sysctl.d/99-veil-quic.conf >/dev/null 2>&1 || true
fi

install -d -m 0750 -o root -g veil /etc/veil
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
install -d -m 0750 -o veil -g veil /var/lib/veil/www
chown -R veil:veil /var/lib/veil/www
find /var/lib/veil/www -type d -exec chmod 0750 {} \;
find /var/lib/veil/www -type f -exec chmod 0640 {} \;
for dir in backups promotion-backups migration-backups; do
    install -d -m 0700 -o root -g root "/var/lib/veil/$dir"
    chown -R root:root "/var/lib/veil/$dir"
    find "/var/lib/veil/$dir" -type d -exec chmod 0700 {} \;
    find "/var/lib/veil/$dir" -type f -exec chmod 0600 {} \;
done
for file in /var/lib/veil/state.json /var/lib/veil/sessions.json; do
    if [ -f "$file" ] && [ ! -L "$file" ]; then
        chown veil:veil "$file"
        chmod 0600 "$file"
    fi
done
for dir in /etc/veil/generated /etc/veil/tls; do
    if [ -d "$dir" ] && [ ! -L "$dir" ]; then
        chown -R root:veil "$dir"
        find "$dir" -type d -exec chmod 0750 {} \;
        find "$dir" -type f -exec chmod 0640 {} \;
    fi
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

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
    systemctl enable veil-helper.socket >/dev/null 2>&1 || true
fi

echo "Veil installed. Run 'veil install' to configure Panel access, or"
echo "'veil doctor' to check host readiness."
