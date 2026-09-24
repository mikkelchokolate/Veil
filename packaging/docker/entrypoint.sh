#!/bin/sh
set -eu

# Leaf paths derive from the root env contract (#659/#672): an operator who
# mounts custom roots via VEIL_VAR_DIR/VEIL_ETC_DIR must not have packaged
# /var/lib/veil + /etc/veil leaf pins override them. An explicit leaf env
# (VEIL_STATE_PATH/…) still wins over the derived default.
VEIL_STATE_PATH="${VEIL_STATE_PATH:-${VEIL_VAR_DIR:-/var/lib/veil}/state.json}"
VEIL_APPLY_ROOT="${VEIL_APPLY_ROOT:-${VEIL_VAR_DIR:-/var/lib/veil}/staging}"
VEIL_KEY_PATH="${VEIL_KEY_PATH:-${VEIL_ETC_DIR:-/etc/veil}/state.key}"
# The health contract is written by `veil serve` under the same state root; a
# custom VEIL_VAR_DIR must move it too (#753). HEALTHCHECK execs bypass this
# entrypoint, so `veil healthcheck` derives the identical path itself.
VEIL_CONTAINER_HEALTH_PATH="${VEIL_CONTAINER_HEALTH_PATH:-${VEIL_VAR_DIR:-/var/lib/veil}/container-health.json}"
export VEIL_STATE_PATH VEIL_APPLY_ROOT VEIL_KEY_PATH VEIL_CONTAINER_HEALTH_PATH

exec /usr/local/bin/veil "$@"
