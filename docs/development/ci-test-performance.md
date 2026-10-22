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

## Machine sharding (test job split across runners)

The orchestrator was CPU-bound on one 4-vCPU hosted runner (~15 min of the
~19 min `test` job). `ci.yml` now runs `test-shard (1..3)`: each leg computes
the identical full task plan (12 API lanes + every package + the serial lane),
LPT-assigns tasks to machines by weight, and runs only its own slice
(`CI_TEST_PHASE=shard`). Leg 1 also owns tidy/gofmt/vet/OpenAPI/SDK checks and
the SDK suite. The required `test` job (`CI_TEST_PHASE=aggregate`) fails closed
unless every leg succeeded, merges task logs and coverprofiles, rejects plan
mismatches, duplicates, or any planned task without a log, then runs the
unchanged API root verification, coverage merge/gates, and inventory report.
`make ci` keeps `CI_TEST_PHASE=all` (the single-machine path). Tagged releases
reuse successful hosted CI and CodeQL for the exact tagged main commit instead
of running these source checks again; see [the release gate](ci.md#tagged-releases).

## Reusing race builds across hosted runs

The [successful main run 39749933809](https://github.com/mikkelchokolate/Veil/actions/runs/39749933809)
on 2026-10-22 took about 14 minutes. Its slowest backend machine started
the orchestrator at 08:47:52 UTC, but the first test event in `api-shard-001`
was at 08:51:36 UTC: about 225 seconds passed before assertions began.
That task took 415 seconds overall; a later API lane on the same machine
took 137 seconds including execution.

All Go jobs previously shared setup-go's immutable cache key based on the
toolchain and `go.sum`. A quick job could populate that cache without the
race/coverage objects; subsequent exact cache hits prevented the longer test
jobs from saving their completed builds. Each backend machine now restores
and saves an additional Go build cache keyed by machine, toolchain/dependency
pins, and source SHA, restoring the newest compatible source cache. The
existing module cache remains in use. Go still validates build objects
against source and flags, and every test still runs with `-race -count=1`
and coverage. The first run on a new cache family remains cold; an updated
hosted run is required to measure the reduction.

The same run also failed to save the validator-runtime cache on every
machine: tar could not read the installer's root-only lock and provenance
files. Validator preparation consequently took 63–82 seconds per machine.
The cache now contains only the public Caddy and sing-box binaries. Its key
tracks the runtime/tool pins and provisioning script, not unrelated installer
or test edits. Reuse checks Caddy's embedded Caddy/forwardproxy module pins,
the forward-proxy module, and sing-box's exact pinned version. The installer's
private metadata keeps its original permissions.

The privilege-boundary, protocol E2E, and both package-smoke machines also
stop installing Node/pnpm: they restore the source-verified frontend build
and never invoke either tool. In the same baseline run those unused setup
steps consumed 13, 14, 17, and 10 seconds respectively. Frontend, browser,
and backend test machines retain their required Node setup.

## Measured scheduling baseline (2026-10-22)

[Main run 39760774474](https://github.com/mikkelchokolate/Veil/actions/runs/39760774474)
took 14m04s. First API tasks spent roughly 139–213 seconds compiling before
execution; later tasks reused that work. Real test time also matters:
runtimeinstall took 334 seconds and backup 281 seconds, while API lanes
ranged from 101 to 253 seconds. Increasing lane count alone cannot remove
these package-level bottlenecks, so the bounded 12-lane plan is retained.

Of runtimeinstall's time, 324.57 seconds came from `TestWithDefaultsBuildCaddy`:
its fake provisioning callback was bypassed by a suitable host Go, causing an
unintended real network build. The test now selects its fake toolchain through
the existing seam and checks the complete command sequence, module pins,
readonly/static build flags, environment, and produced binary. It takes 0.04
seconds locally under race instrumentation; live install acceptance retains
the real build. The seed adjusts this package's obsolete wall-time estimate
by that measured difference; the next hosted manifest replaces it.

The checked-in timing seed records the source run, slow API roots (at least
five seconds), and measured package process wall times. A successful aggregate
now preserves every shard's task timings and all observed roots, including
zero-duration roots. Otherwise package times became zero and fast roots
incorrectly received the unknown-root fallback. The verified full manifest
is cached after inventory and coverage gates pass. The frontend job restores
it once and publishes one snapshot with the shared source-keyed artifact;
every machine therefore computes the same plan, even if another run finishes
while this run is starting. Missing caches use the measured seed; new roots
keep the existing fallback. No tests, race checks, coverage, or inventory gates
are removed.

[The first cold cache run 39762673218](https://github.com/mikkelchokolate/Veil/actions/runs/39762673218)
passed all three shards and the aggregate, and successfully saved both the
race/coverage build caches and validator binaries. A subsequent warm hosted
run is still required before claiming an end-to-end speedup.

## First-use runtime acceptance

`install-acceptance.sh` starts without server runtimes on PATH, including in
the local system image. It no longer copies the prebuilt runtime set or runs
an eager `veil runtime install`. It checks that a direct panel-only install
and a disabled inbound leave every protocol binary absent. Enabling the
first Hysteria2 inbound must install only Hysteria2; adding Mieru and olcRTC
must add only their binaries. The first WARP enable supplies generated test
WireGuard keys, requires the pinned sing-box, validates the generated config,
and checks the live service. This verifies installation and startup; the
test keys do not establish a real Cloudflare VPN session. Disabling WARP
must stop its service. The later Caddy panel-access leg still requires Caddy.

The local full-system phase runs install acceptance first, before the
protocol E2E job seeds binaries. Optional-runtime stash/restore continues
checking uninstall cleanup without assuming unused Caddy is already present.
