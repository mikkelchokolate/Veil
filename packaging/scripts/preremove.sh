#!/bin/sh
# Veil package preremove: stop and disable managed units before files are removed.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl stop veil-backup.timer >/dev/null 2>&1 || true
    systemctl disable veil-backup.timer >/dev/null 2>&1 || true
    systemctl stop veil.service veil-helper.service veil-helper.socket >/dev/null 2>&1 || true
    systemctl disable veil.service veil-helper.socket >/dev/null 2>&1 || true
    for unit in veil-caddy@ veil-hysteria2@ veil-mieru veil-olcrtc@ veil-warp; do
        systemctl stop "${unit}.service" >/dev/null 2>&1 || true
        systemctl disable "${unit}.service" >/dev/null 2>&1 || true
    done
fi
