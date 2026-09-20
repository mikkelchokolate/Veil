#!/usr/bin/env bash
# scripts/ci/gh-release-verify.sh — download a GitHub release asset with the
# SHA256 GitHub recorded for it verified BEFORE the caller unpacks or installs
# anything (#472).
#
# Library only — source it, then call gh_release_download_verified. Used by the
# scheduled upstream-compat job, which intentionally tracks /releases/latest:
# "latest" cannot carry a versions.sh pin, so the release API's asset digest is
# the trust anchor. A missing/malformed digest fails closed — an unverifiable
# binary is never installed.
#
# Requires: gh (authenticated — GH_TOKEN), sha256sum.

# gh_release_download_verified <owner/repo> <name-prefix> <name-suffix> <dir>
#
# Resolves the first asset of the repo's latest release whose name starts with
# <name-prefix> AND ends with <name-suffix>, downloads it into <dir>, and
# verifies its sha256 against the digest published by the GitHub release API.
# Pass the same value twice for an exact-name match.
gh_release_download_verified() {
  local repo="$1" prefix="$2" suffix="$3" dir="$4"
  local meta name digest

  meta="$(gh api "repos/${repo}/releases/latest" \
    --jq "[.assets[] | select(.name | startswith(\"${prefix}\") and endswith(\"${suffix}\"))] | first // empty | .name + \"\t\" + (.digest // \"\")")"
  if [ -z "${meta}" ]; then
    printf '[ci] ERROR: no asset matching %s...%s in latest %s release\n' "${prefix}" "${suffix}" "${repo}" >&2
    return 1
  fi
  name="${meta%%$'\t'*}"
  digest="${meta##*$'\t'}"
  if [[ ! "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    printf '[ci] ERROR: %s asset %s publishes no sha256 digest (%s) — refusing unverified install\n' "${repo}" "${name}" "${digest:-none}" >&2
    return 1
  fi

  mkdir -p "${dir}"
  gh release download --repo "${repo}" --pattern "${name}" --dir "${dir}" --clobber
  printf '%s  %s/%s\n' "${digest#sha256:}" "${dir}" "${name}" | sha256sum --check -
}
