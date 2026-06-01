#!/bin/sh
# Veil package postinstall: reload systemd so the managed units are visible.
# Veil install (panel access, credentials, Caddy/TLS) is run separately by the
# operator via `veil install`; packages only deliver the binary and units.
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

echo "Veil installed. Run 'veil install' to configure Panel access, or"
echo "'veil doctor' to check host readiness."
