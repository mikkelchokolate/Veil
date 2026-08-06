# CI `test` performance work

## Scope and status

This change is limited to the GitHub Actions `test` job and its test-only
orchestration/support code. The source HEAD at investigation start was
`320634ad9b7892e6690ea6921fb180bce59cec56`; no reset or unrelated-workflow
change was made.

The implementation is **not yet acceptance-complete**: the host filesystem
reached 100% during cold full-run profiling, and the no-`-short` API suite still
has a residual lifecycle cost above the requested target. The numbers below are
therefore measurements, not a claim that the 8:30/12:00 targets were met.

## Implemented changes

- Added `scripts/ci/test-inventory.py`.
  - Discovers roots from `go list -json` and active `TestGoFiles`/`XTestGoFiles`.
  - Cross-checks executed JSON events and rejects missing/unexpected roots.
  - Emits `test-roots.json`, `test-timings.json`, `package-timings.json`,
    `slow-tests.txt`, `expected-roots.txt`, `executed-roots.txt`,
    `shard-plan.json`, `shard-balance.txt`, and `timing-manifest.json`.
- Added `scripts/ci/test-orchestrator.py`.
  - One bounded worker pool controlled by `CI_TEST_WORKERS` (default
    `min(nproc, 4)`).
  - API roots are grouped by LPT; package and API tasks share the same pool.
  - No retry; each root batch has one execution and nested subtests remain in
    their root process.
  - Race, `-count=1`, JSON logs, coverage, and timeouts remain enabled.
- `test.sh` now records machine-readable stage timings and merges per-task
  coverage profiles. It preserves the source-keyed frontend validation and
  all required verification gates.
- Test-job runtime preparation now installs/validates only pinned Caddy with
  `http.handlers.forward_proxy`; full runtime installation remains available to
  protocol E2E jobs. The GitHub test job has an exact Caddy cache key and no
  Node/pnpm setup.
- Added `internal/testutil/testdb`: one production-created, checkpointed,
  closed schema template per test process; clones are byte copies with isolated
  writable files and schema fingerprint validation. Fresh migration/corruption
  fixtures continue using production `storage.Open` when they pre-create the
  database.
- Added scoped `CryptoOptions.DeriveKey` for backup operations and removed the
  mutable `deriveKeyHook` seam. Production defaults still use the real KDF;
  test operations can use deterministic derivation. Legacy/v2 production KDF
  coverage remains represented by compatibility tests.
- Added scoped password-hasher dependencies for CLI/API operations. Production
  constructors retain bcrypt default cost; ordinary command/API tests use
  MinCost through test constructors, with an explicit production-policy test.
- API test routers/states inject the prepared clone and fast test hasher without
  changing production `NewRouter` defaults.

## Measurements

Provided baseline on the investigated HEAD:

- `internal/client`: 372.552s
- `internal/backup`: 250.065s
- `internal/cli`: 237.338s
- `internal/privileged`: 156.335s
- `internal/apply`: 153.825s
- `internal/runtimeinstall`: 96.867s
- `internal/statecommit`: 90.180s
- `internal/storage`: 30.361s
- `internal/api`: approximately 258s after the non-API phase
- coverage: 88.1%

Observed during this work:

- Static inventory: 2.5s and API roots matched `go test -list` exactly (`704`
  after adding the production bcrypt-policy test; the original inventory was
  `703`).
- Prior sequential discovery: 102.4s; this was removed.
- Cold full-run stage samples: frontend 9.5s, tidy 2.8s, gofmt 0.4s,
  OpenAPI 3.5s, SDK verification 11.7s, build 0.6s, Caddy 0.1s, SDK tests
  39.9s, discovery 2.5s.
- Cold package samples after orchestration: client 350.477s, backup
  438.320s, CLI 264.070s in an isolated package run, API shards observed at
  583.522s, 271.872s, and 301.892s under the no-`-short` suite.
- After mocking the test-only runtime installer seam in the confirmed CLI
  install test, that root fell from 631.33s to 3.40s; the full isolated CLI
  package then passed in 264.07s.

The API residual is the remaining critical path: the expensive roots are
lifecycle/traffic/apply scenarios, not just shard imbalance. A successful
three-run cold/warm/warm matrix and hosted-SHA verification still need to be
completed on a runner with sufficient free disk.

## Verification performed

Passed:

```text
git rev-parse HEAD
git status --short   # clean at start
git diff --check
bash -n scripts/ci/test.sh scripts/ci/runtimes.sh
python3 -m py_compile scripts/ci/test-inventory.py scripts/ci/test-orchestrator.py
go test ./... -run '^$' -count=1
go test ./internal/testutil/testdb -race -count=1
go test ./internal/api -race -count=1 -run 'selected router/setup/settings roots'
go test ./internal/cli -race -count=1 -run '^TestInstallRURecommendedApplyWritesFilesWhenConfirmed$'
go test ./internal/cli -race -count=1
```

Not completed successfully in this environment:

```text
bash scripts/ci/test.sh                 # full no-short run remains above target
CI_BACKEND=docker make ci-job JOB=test  # not run after final changes
3-run cold/warm/warm matrix             # blocked by disk pressure/residual API time
hosted GitHub Actions exact-SHA check   # not available from this environment
```

The artifacts generated by each successful `scripts/ci/test.sh` invocation are
under its configured `CI_ARTIFACT_DIR`; the orchestrator intentionally removes
stale task logs at the beginning of a run.
