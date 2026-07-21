#!/usr/bin/env bash
# scripts/ci/stress.sh — `make ci-stress`: determinism proof for the historically
# environment-dependent tests. Runs the exact commands from the CI parity spec.
set -euo pipefail

_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/ci/common.sh
. "${_script_dir}/common.sh"

cd "${CI_ROOT}"

ci_step "web/dist (embedded into the binary)"
(cd web && pnpm install --frozen-lockfile && pnpm build)

runtime_dir="${VEIL_CI_RUNTIME_DIR:-/opt/veil-runtime}"
if [ ! -x "${runtime_dir}/caddy" ]; then
  runtime_dir="$(mktemp -d /tmp/veil-runtime.XXXXXX)"
  go build -ldflags "-X main.version=stress" -o /tmp/veil-stress-bin ./cmd/veil
  # shellcheck source=scripts/ci/runtimes.sh
  . "${CI_SCRIPTS_DIR}/runtimes.sh"
  install_pinned_runtimes "${runtime_dir}" "/tmp/veil-stress-bin"
fi
export PATH="${runtime_dir}:${PATH}"

ci_step "stress: full internal/api x20 (-race -shuffle=on)"
# -timeout: the go test default (10m) is calibrated for -count=1; multiplied
# -race runs legitimately need more. This is a harness budget, not a retry.
ci_run stress-api-x20 go test ./internal/api -race -count=20 -shuffle=on -timeout 60m -v

ci_step "stress: historically flaky tests x50 (-race -shuffle=on)"
ci_run stress-flaky-x50 go test ./internal/api -race -count=50 -shuffle=on -timeout 60m \
  -run 'TestApplyStateTracksDesiredVsApplied|TestMutationResponseIncludesRevisionAndApplyJob|TestStartupMigrateLegacyRestoredStateMigratesNewProfiles|TestClientMutationOrchestration' -v

ci_log "stress job passed"
