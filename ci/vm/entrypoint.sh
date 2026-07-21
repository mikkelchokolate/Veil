#!/usr/bin/env bash
# ci/vm/entrypoint.sh — image ENTRYPOINT. Delegates to guest-run.sh.
exec /opt/ci/guest-run.sh "$@"
