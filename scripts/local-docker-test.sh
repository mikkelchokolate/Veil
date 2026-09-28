#!/usr/bin/env bash
# Local helper: run Go tests in docker on an LF-normalized copy of the working
# tree. The Windows working copy is CRLF; several regression tests scan source
# with "\n" literals, so we copy the tree and strip CR line-endings. Tests run
# as `nobody` so the privileged executor's root-only chown path is skipped,
# matching CI runners.
# Usage: scripts/local-docker-test.sh <go test args...>
#   TEST_RUN='TestA|TestB' scripts/local-docker-test.sh ./internal/api/ -count=1 -v
# TEST_RUN and the go-test args are carried through docker as env vars
# (%q-quoted for args) so | alternation, quotes and spaces survive every
# shell layer without composing an eval'd command string (#1149).
set -euo pipefail

# Repo root is the script's parent directory — never a hardcoded machine
# path. git-bash needs the Windows form (E:/...) for the bind mount; cygpath
# exists there, plain POSIX paths are used elsewhere (#1149).
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if command -v cygpath >/dev/null 2>&1; then
	repo_root="$(cygpath -m "${repo_root}")"
fi

# Encode the positional args once, host-side; the inner script evals this
# single string. %q output re-splits to exactly the original argv.
args_q="$(printf '%q ' "$@")"

MSYS_NO_PATHCONV=1 docker run --rm -i \
	-e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOFLAGS=-mod=mod \
	-e "TEST_RUN=${TEST_RUN:-}" \
	-e "TEST_ARGS_Q=${args_q}" \
	-v "${repo_root}:/src:ro" \
	-v veil-gomod:/go/pkg/mod \
	-w /tmp golang:1.27.1 bash -s <<'INNER'
set -euo pipefail
mkdir -p /tmp/work /tmp/gocache /tmp/gomodcache
cp -a /src/. /tmp/work/
chmod -R a+rwX /tmp/work /tmp/gocache /tmp/gomodcache
cd /tmp/work
find . -type f \( -name '*.go' -o -name '*.yaml' -o -name '*.yml' -o -name '*.ts' -o -name '*.tsx' -o -name '*.js' -o -name '*.json' -o -name '*.md' -o -name '*.sh' -o -name '*.mod' -o -name '*.sum' -o -name '*.html' -o -name '*.css' -o -name '*.sql' \) -exec sed -i 's/\r$//' {} +
chmod -R a+rwX /go/pkg/mod 2>/dev/null || true
# Rebuild argv from the %q-encoded host args (default: the unit-test tree).
eval "set -- ${TEST_ARGS_Q:-./internal/...}"
cmd=(go test)
if [ -n "${TEST_RUN:-}" ]; then cmd+=(-run "${TEST_RUN}"); fi
cmd+=("$@")
# su -c takes a string — printf %q re-quotes it exactly once, so a quote or
# space inside TEST_RUN or an arg cannot break the inner shell (#1149).
su -s /bin/bash nobody -c "cd /tmp/work && HOME=/tmp GOCACHE=/tmp/gocache $(printf '%q ' "${cmd[@]}")"
INNER
