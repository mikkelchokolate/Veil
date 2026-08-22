#!/usr/bin/env bash
# scripts/ci/fast.sh — `make ci-fast`: quick pre-commit checks ON THE HOST.
# This is intentionally NOT a full CI reproduction; it catches the cheap
# classes of breakage in seconds. `make ci` is required before pushing.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"
ci_timer_start

ci_step "go.mod tidy drift"
go mod tidy
git diff --exit-code -- go.mod go.sum

ci_step "gofmt"
unformatted="$(git ls-files '*.go' | xargs gofmt -l)"
[ -z "${unformatted}" ] || { printf 'not gofmt-clean:\n%s\n' "${unformatted}" >&2; exit 1; }

ci_run go-vet go vet ./...
ci_run verify-sdk make verify-sdk
ci_run verify-openapi make verify-openapi

ci_step "frontend (frozen install, generated drift, typecheck, biome, i18n, unit)"
(cd web && pnpm install --frozen-lockfile)
(cd web && pnpm gen && git diff --exit-code -- src/api/generated)
ci_run frontend-typecheck pnpm --dir web typecheck
ci_run frontend-biome pnpm --dir web check
ci_run frontend-i18n pnpm --dir web i18n:check
ci_run frontend-unit pnpm --dir web test

ci_step "fast Go unit tests (short mode, no race)"
go test ./... -short -count=1 -timeout=20m

ci_step "shell syntax"
bash -n scripts/*.sh scripts/ci/*.sh
sh -n packaging/scripts/*.sh

ci_step "git diff --check"
git diff --check

ci_timer_stop "ci-fast"

cat <<'EOF'

ci-fast passed.

This is not a full CI reproduction.
Run `make ci` before pushing.
EOF
