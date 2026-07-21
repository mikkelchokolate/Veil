#!/usr/bin/env bash
# ci/vm/prepare-workspace.sh — runs INSIDE the CI guest.
# Extracts the repository snapshot onto the native Linux filesystem at
# /workspace/veil, creates a fresh git repository (so generated-file drift
# checks work), and fixes ownership for the ci user.
#
# Usage: prepare-workspace.sh /exchange/snapshot.tar
set -euo pipefail

SNAPSHOT="${1:?usage: prepare-workspace.sh <snapshot.tar>}"
# In the docker-backend system VM the workspace lives under the exchange dir
# at an ABSOLUTELY IDENTICAL path on host and guest, so `docker run -v
# $repo/...` issued by package-smoke resolves on the host daemon (Docker-out-
# of-Docker path alignment).
WORKSPACE="${CI_WORKSPACE_OVERRIDE:-/workspace/veil}"

rm -rf "${WORKSPACE}"
mkdir -p "${WORKSPACE}"
tar -xf "${SNAPSHOT}" -C "${WORKSPACE}"

cd "${WORKSPACE}"
git init -q
git config user.name local-ci
git config user.email local-ci@invalid
git add -A
git commit -qm "CI snapshot"

chown -R ci:ci /workspace

echo "workspace ready: ${WORKSPACE} ($(du -sh "${WORKSPACE}" | cut -f1), native fs: $(stat -f -c %T "${WORKSPACE}"))"
