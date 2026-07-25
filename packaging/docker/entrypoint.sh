#!/bin/sh
set -eu

VEIL_STATE_PATH="${VEIL_STATE_PATH:-/var/lib/veil/state.json}"
VEIL_APPLY_ROOT="${VEIL_APPLY_ROOT:-/etc/veil}"
VEIL_KEY_PATH="${VEIL_KEY_PATH:-/etc/veil/state.key}"
export VEIL_STATE_PATH VEIL_APPLY_ROOT VEIL_KEY_PATH

exec /usr/local/bin/veil "$@"
