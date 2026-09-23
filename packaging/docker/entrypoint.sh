#!/bin/sh
set -eu

# Leaf paths derive from the root env contract (#659/#672): an operator who
# mounts custom roots via VEIL_VAR_DIR/VEIL_ETC_DIR must not have packaged
# /var/lib/veil + /etc/veil leaf pins override them. An explicit leaf env
# (VEIL_STATE_PATH/…) still wins over the derived default.
VEIL_STATE_PATH="${VEIL_STATE_PATH:-${VEIL_VAR_DIR:-/var/lib/veil}/state.json}"
VEIL_APPLY_ROOT="${VEIL_APPLY_ROOT:-${VEIL_VAR_DIR:-/var/lib/veil}/staging}"
VEIL_KEY_PATH="${VEIL_KEY_PATH:-${VEIL_ETC_DIR:-/etc/veil}/state.key}"
export VEIL_STATE_PATH VEIL_APPLY_ROOT VEIL_KEY_PATH

exec /usr/local/bin/veil "$@"
