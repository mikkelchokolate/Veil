#!/usr/bin/env bash
# Decide which floating "latest" channel a release tag may claim.
#
# One source of truth for both channels: the GHCR `:latest` image tag (the
# docker-publish job) and the GitHub "Latest" release (the publish job). A
# prerelease claims neither, and a stable tag that is older than the newest
# published stable release — a backport cut after a newer stable line — claims
# neither (#1148, #1414, #1422, #1448).
#
# Usage: release-channel.sh <tag>
# Env:   GITHUB_REPOSITORY (owner/repo), GH_TOKEN with release read access.
# Output (stdout, key=value lines):
#   channel=latest      stable and not older than the newest published stable
#   channel=backport    stable but older than the newest published stable
#   channel=prerelease  not an exact vMAJOR.MINOR.PATCH tag
#   newest=<tag>        only with channel=backport: the anchor compared against
# Exits non-zero when the release list cannot be read, so callers fail closed
# instead of guessing which channel to move.
set -euo pipefail

stable_re='^v[0-9]+\.[0-9]+\.[0-9]+$'
tag="${1:?usage: release-channel.sh <tag>}"
repo="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY must be set}"

if [[ ! "${tag}" =~ ${stable_re} ]]; then
    echo "channel=prerelease"
    exit 0
fi

# The anchor is the highest stable-semver tag among published non-prerelease
# releases, not whichever release carries the Latest flag: a mis-flagged
# Latest (an RC promoted before #1414) must neither block nor mislead the
# comparison (#1422).
published="$(gh release list --repo "${repo}" \
    --exclude-drafts --exclude-pre-releases --limit 1000 \
    --json tagName --jq '.[].tagName')"

newest_stable=""
while IFS= read -r candidate; do
    [[ "${candidate}" =~ ${stable_re} ]] || continue
    if [[ -z "${newest_stable}" ]] \
        || [[ "$(printf '%s\n%s\n' "${newest_stable#v}" "${candidate#v}" | sort -V | tail -n 1)" == "${candidate#v}" ]]; then
        newest_stable="${candidate}"
    fi
done <<< "${published}"

# Version ordering, not lexical: sort -V puts v0.10.0 above v0.9.9.
if [[ -z "${newest_stable}" ]] \
    || [[ "$(printf '%s\n%s\n' "${newest_stable#v}" "${tag#v}" | sort -V | tail -n 1)" == "${tag#v}" ]]; then
    echo "channel=latest"
    exit 0
fi

echo "channel=backport"
echo "newest=${newest_stable}"
