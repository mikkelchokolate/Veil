#!/usr/bin/env bash
# Local helper: run Go tests in docker on an LF-normalized copy of the working
# tree. The Windows working copy is CRLF; several regression tests scan source
# with "\n" literals, so we copy the tree and strip CR line-endings. Tests run
# as `nobody` so the privileged executor's root-only chown path is skipped,
# matching CI runners.
# Usage: scripts/local-docker-test.sh <go test args...>
#   TEST_RUN='TestA|TestB' scripts/local-docker-test.sh ./internal/api/ -count=1 -v
# TEST_RUN carries the -run pattern through docker as an env var so | regex
# alternation survives the extra shell layers.
set -euo pipefail
args="${*:-./internal/...}"
MSYS_NO_PATHCONV=1 docker run --rm \
	-e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOFLAGS=-mod=mod \
	-e "TEST_RUN=${TEST_RUN:-}" \
	-v "E:\Projects\devin\Veil-batch-k:/src:ro" \
	-v veil-gomod:/go/pkg/mod \
	-w /tmp golang:1.27.1 bash -c "
set -euo pipefail
mkdir -p /tmp/work /tmp/gocache /tmp/gomodcache
cp -a /src/. /tmp/work/
chmod -R a+rwX /tmp/work /tmp/gocache /tmp/gomodcache
cd /tmp/work
find . -type f \( -name '*.go' -o -name '*.yaml' -o -name '*.yml' -o -name '*.ts' -o -name '*.tsx' -o -name '*.js' -o -name '*.json' -o -name '*.md' -o -name '*.sh' -o -name '*.mod' -o -name '*.sum' -o -name '*.html' -o -name '*.css' -o -name '*.sql' \) -exec sed -i 's/\r$//' {} +
chmod -R a+rwX /go/pkg/mod 2>/dev/null || true
test_cmd='cd /tmp/work && HOME=/tmp GOCACHE=/tmp/gocache go test'
if [ -n \"\${TEST_RUN:-}\" ]; then test_cmd=\"\$test_cmd -run '\$TEST_RUN'\"; fi
test_cmd=\"\$test_cmd ${args}\"
su -s /bin/bash nobody -c \"\$test_cmd\"
"
