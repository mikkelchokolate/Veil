#!/bin/sh
# Veil package postremove: reload systemd after unit files are removed.
# State under /etc/veil is intentionally preserved so reinstalling Veil keeps
# Panel credentials and generated configs. Use `veil uninstall` for full removal.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi
