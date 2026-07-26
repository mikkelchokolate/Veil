# Local CI — reproducing GitHub Actions before push

This document describes the local CI system: why it exists, how it works, and
how to troubleshoot it.

## Why `go test ./...` is not enough

For a long time the local verification story was essentially:

```bash
go test ./...
```

while GitHub Actions ran a strictly stronger gate:

```bash
packages="$(go list ./... | grep -v '/sdk/go$')"
go test ${packages} -race -count=1 -coverprofile=coverage.out
```

plus frontend build, generated-file drift checks, lint + vulnerability scans,
browser tests, privilege tests, a coverage threshold, package smoke and an OCI
image build — in a clean Ubuntu 24.04 environment, and for pull requests on a
**temporary merge with the target branch**. A green local `go test ./...`
therefore did not imply a green GitHub CI. Concretely, this repo experienced:

- apply tests that passed as local root and failed in CI as an unprivileged
  user (they drove the real `ufw`);
- a migration backup collision that only appeared when the suite ran twice in
  the same second on a fresh machine;
- `web/dist` embed failures because the SPA was never built before Go builds
  locally;
- workflow/Dockerfile drift (missing `.npmrc`, missing dist stub) that no local
  command exercised at all.

The rules below exist so that **the same failure shows up before push, not
after**.

### Why `-race`

Data races are nondeterministic: a racy test can pass 100 times locally and
fail on a differently scheduled CI machine. The race detector turns latent
races into immediate failures. Both the SDK and product suites run with it.

### Why `-count=1`

Go caches test results. Without `-count=1`, re-running the suite may replay
cached successes and never execute the code you just changed. CI always runs
uncached; local CI does the same.

### Why the merge tree

For `pull_request` events GitHub checks out a synthetic merge of your branch
into the base branch. If `main` moved since you branched, "green on my branch"
and "green when merged" are different statements. `make ci-pr` reproduces the
same temporary merge locally (detached worktree, `--no-ff` merge of HEAD into
`origin/main`) and runs the full gate on that tree. Your branch and working
copy are never modified and nothing is pushed.

## The parity contract

The local VM and GitHub Actions deliberately match on:

- the checked-out git tree (commit or merge tree);
- the exact CI scripts (`scripts/ci/*.sh` — the only place job logic lives);
- Ubuntu 24.04 user-space and glibc;
- Go, Node, pnpm and CI tool versions (single source: `scripts/ci/versions.sh`);
- Go module transport (`GODEBUG=http2client=0`) to avoid known HTTP/2 stalls on
  clean-cache downloads while keeping the same setting in local and GitHub jobs;
- protocol runtime versions (pinned + SHA256-verified in the CI image);
- test flags (`-race -count=1 -coverprofile`, tags, timeouts);
- coverage threshold (70%, never lowered);
- generated-file drift checks;
- locale (`C.UTF-8`), timezone (`UTC`);
- environment variables that influence tests;
- root/non-root execution model (unit jobs run as user `ci`, UID 1000; root is
  reserved for system integration);
- working directories and the sequence of mandatory checks.

Intentional differences (recorded in the environment manifest of every run):

- kernel version, hypervisor, CPU model;
- the set of preinstalled OS packages beyond what the images declare;
- GitHub's network infrastructure and runner internals.

Every run writes `.artifacts/ci/environment-<job>.txt` so differences are
auditable instead of surprising.

## Why a VM

Containers share the host kernel and (often) the host filesystem semantics.
The failure modes this system exists to catch — privilege boundaries, systemd
unit behaviour, Unix ownership/permission matrices, socket activation — are
exactly the ones a shared-kernel container can mask. Running the gate inside a
hardware-isolated microVM with its own kernel removes the host from the
equation: what passes is what the code does, not what the host tolerates.

## Why smolvm

[smolvm](https://github.com/smol-machines/smolvm) boots OCI images as microVMs
(libkrun: KVM on Linux, Hypervisor.framework on macOS, Windows Hypervisor
Platform on Windows) with sub-second cold starts, ephemeral machines, copy
in/out and no daemon. The CI images are ordinary OCI images, so the same
artifacts work for local VM runs, the diagnostic docker backend, and any
future registry-based distribution.

## Why Ubuntu 24.04 OCI (not a full VM image, not Alpine)

- GitHub-hosted runners are Ubuntu 24.04. Matching the user-space (glibc,
  coreutils, systemd) is the point of the parity contract.
- A full Ubuntu Server VM image drags in a kernel, bootloader, cloud-init and
  hundreds of packages CI never touches, and boots orders of magnitude slower.
  An OCI rootfs with exactly the audited package set is minimal *and*
  reproducible — and smolvm supplies its own guest kernel.
- Alpine is musl-based. The Go race detector, Chromium, systemd, and several
  protocol runtimes are glibc targets; a musl CI image would silently test a
  different platform than production.

## Image architecture

One canonical base, two purpose-built derivatives, shared OCI layers:

```
ubuntu:24.04@sha256:<pinned>
        │
        ▼
  veil-ci-base        bash, ca-certificates, git, make, gcc + libc headers,
        │             Go, Node, pnpm, curl, jq, tar/xz, shellcheck, sudo,
        │             staticcheck, govulncheck, nfpm, redocly
        ├── veil-ci-browser   + pinned Playwright, Chromium, headless deps,
        │                     minimal fonts
        └── veil-ci-system    + systemd, D-Bus, ufw/nftables, dpkg-dev/rpm,
                              docker CLI, pinned protocol runtimes
                              (hysteria2, mita, mieru, naive, sing-box, caddy)
```

- `base` runs: frontend, test, lint, stress.
- `browser` runs: browser-e2e.
- `system` runs in a booted systemd smolvm guest: privilege-boundary and e2e.
- `package-smoke` and `image-build` require an OCI daemon. They run through an
  explicit host Docker backend even when `CI_BACKEND=smolvm`; their logs identify
  that boundary and never claim those jobs ran inside smolvm.

Not a monolith: the browser job does not carry systemd, the system jobs do not
carry Chromium, and base jobs carry neither. GCC and libc headers stay in
`base` because `go test -race` requires CGO. For smolvm, each OCI image is
flattened once with `docker export` into a content-keyed rootfs directory in the
HDD-backed cache. This avoids guest-side docker-archive extraction while
preserving the exact built filesystem; Docker-backed jobs continue to use the
OCI image directly. Local smolvm runs mount the snapshot and Go/pnpm caches from
the HDD-backed cache into the guest; no test state is persisted.

All downloads (Go, Node, runtimes, docker CLI) are SHA256-verified before
unpacking — see `scripts/ci/versions.sh` and `ci/vm/Containerfile`.

## Commands

```bash
make ci-fast    # pre-commit: quick host checks (seconds). NOT a full CI.
make ci         # pre-push: VM-capable jobs in smolvm; image-build via host Docker
make ci-full    # adds browser/systemd VM jobs and host-Docker package smoke
make ci-pr      # ci-full on the temporary merge with origin/main
make ci-stress  # race/shuffle stress for historically flaky tests

make ci-job JOB=test            # one job in a VM
make ci-job JOB=e2e
make ci-host                    # diagnostic: standard set directly on the host
make ci-job-host JOB=test       # diagnostic: one job on the host

make ci-image                   # (re)build CI images whose inputs changed
make ci-clean                   # artifacts, temp VMs/worktrees
make ci-clean CI_CLEAN_ARGS=--images   # also drop CI images + archives
```

`ci-host` / `ci-job-host` print a loud warning and are never selected
automatically. They exist for debugging the scripts themselves.

### Environment knobs

| Variable | Default | Meaning |
|---|---|---|
| `CI_BACKEND` | `smolvm` | `smolvm` (authoritative) or `docker` (explicit diagnostic) |
| `CI_CPUS` | `4` | vCPUs per VM |
| `CI_MEMORY` | `8` | GiB RAM per VM |
| `CI_VM_TIMEOUT` | `5400` | seconds per guest run |
| `CI_CLEAN` | `0` | `1` disables dependency caches for the run |
| `CI_ARTIFACT_DIR` | `.artifacts/ci` | where logs/manifests/timings land |

## Virtualization setup

The local VM path currently supports **amd64/x86_64 only** because its pinned
runtime and browser assets are amd64. It requires smolvm >= 1.6.13. Docker CLI
and a reachable host Docker daemon are also required to build/export the
content-keyed OCI rootfs images and to run the explicitly host-backed
`image-build` and `package-smoke` phases.

### Linux (KVM)

`smolvm` requires `/dev/kvm`:

```bash
ls /dev/kvm || sudo modprobe kvm kvm_intel   # or kvm_amd
curl -sSL https://smolmachines.com/install.sh | bash
```

On a cloud VM without nested virtualization exposed, `/dev/kvm` simply does
not exist and **cannot be enabled from inside the guest**. In that case run CI
on a KVM-capable machine, or use the explicit diagnostic backend:

```bash
CI_BACKEND=docker make ci
```

which runs the same images/scripts under the docker daemon (no hardware
isolation, host kernel — clearly warned, never automatic).

### What GitHub-hosted green CI proves

Normal GitHub jobs validate the shared `scripts/ci/*` job logic and the
Docker-backed OCI/package phases. They intentionally do **not** repeat the
entire suite through smolvm: that would duplicate every PR job. A green GitHub
workflow is therefore not proof of create/start/systemd/exec/stop orchestration.
The merge gate additionally requires one successful default-backend
`make ci-full` on an amd64 KVM-capable machine for the exact HEAD.

### Windows (WHP / WSL2)

smolvm on Windows uses the Windows Hypervisor Platform (enable the WHP
Windows feature). Under WSL2, install smolvm inside the WSL distribution;
WSL2 exposes `/dev/kvm` on recent Windows 11 builds — verify with
`ls /dev/kvm`. Without it, use `CI_BACKEND=docker` with Docker Desktop's
WSL2 backend.

### macOS

The current repository-owned images/assets are amd64-only, so Apple Silicon is
not supported by the authoritative local backend yet. An x86_64 macOS host may
use Hypervisor.framework if smolvm and Docker satisfy the requirements above.

## Caching

Persisted between runs (safe):

- OCI image layers (content-keyed — rebuilt only when inputs change);
- Go module cache, Go build cache, pnpm store, Playwright browsers
  (`~/.cache/veil-ci/`);
- exported image archives for smolvm.

Never persisted (would poison results): `/etc/veil`, `/var/lib/veil`,
databases, keys, apply state, systemd state, sockets, temp dirs, panel state.

`CI_CLEAN=1 make ci-full` runs with cold dependency caches.
`CI_CLEAN=1 make ci-image` forces a full image rebuild.

## Artifacts

Every run (success or failure) leaves `.artifacts/ci/` with:

- `environment-<job>.txt` — the environment manifest (secrets redacted);
- `<job>.log`, per-step logs, `product-test.log`, `coverage.out`,
  `coverage-summary.txt`;
- `image-key.txt`, `image-metadata.txt` (image sizes);
- `timings.jsonl` — per-step durations;
- `systemd-journal.txt` for system jobs;
- browser panel logs + Playwright reports under `browser-e2e/`;
- `summary` printed on failure with the absolute artifact path.

## Stress mode

`make ci-stress` runs, inside `veil-ci-base`:

```bash
go test ./internal/api -race -count=20 -shuffle=on -v
go test ./internal/api -race -count=50 -shuffle=on \
  -run 'TestApplyState...|TestMutationResponse...|TestStartupMigrate...|TestClientMutationOrchestration' -v
```

These are the tests that historically diverged between host and CI. They are
fixed for real (privilege stubbing, collision-safe backup names) — the stress
run is the regression net, not a retry-until-green mechanism.

## Troubleshooting

### smolvm fails with "kvm not available"

`/dev/kvm` is missing. On bare metal: `sudo modprobe kvm kvm_intel` (or
`kvm_amd`) and check BIOS virtualization. Inside a cloud guest: the outer
hypervisor does not expose nested virtualization — nothing inside can fix it;
use another host or the explicit `CI_BACKEND=docker` diagnostic backend.

### systemd jobs

The system VM must boot systemd as PID 1. Diagnostics:

```bash
docker run -d --privileged --cgroupns=host \
  -v /sys/fs/cgroup:/sys/fs/cgroup:rw --entrypoint /sbin/init veil-ci-system:<key>
docker exec <ctr> systemctl is-system-running
docker exec <ctr> /opt/ci/systemd/poc.sh     # full PoC: service, socket, journal
```

`systemd-analyze verify` is a static check only — the gate is the real
lifecycle PoC plus the socket-activation integration tests.

### Browser jobs

Chromium is preinstalled in `veil-ci-browser` at a pinned Playwright version
(system-wide `PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright`). If a version bump
in `web/package.json` or `test/browser/package.json` diverges from
`CI_PLAYWRIGHT_VERSION`, the image must be rebuilt (`make ci-image`) and the
pin updated — the image key changes automatically.

### Network

smolvm networking is opt-in; CI VMs run with `--net`. Dependency caches make
steady-state runs mostly offline, but first runs and cache misses need
outbound HTTPS (Go modules, npm, GitHub). GitHub rate limits on release
metadata are avoided in required CI because runtimes are pinned inside the
image, not resolved at test time.

### Image build job

`image-build` talks to an OCI daemon. GitHub runners provide one. Local runs,
including default smolvm runs, dispatch this job explicitly to the host Docker
backend; it is not executed in or attributed to the daemonless smolvm guest.

## Known remaining differences vs GitHub runners

- GitHub runners are full VMs with a broad preinstalled toolset; CI images are
  deliberately minimal (the manifest records what exists). One preinstalled
  tool is load-bearing: **GitHub's ubuntu-24.04 image ships `caddy` on PATH**,
  and unit tests probe `caddy list-modules` unconditionally. Every CI target
  therefore carries the pinned forward_proxy caddy in /opt/veil-runtime, and
  `test.sh` puts it on PATH — a difference you only notice in a clean image.
- Kernel differs (libkrunfw guest kernel vs Azure kernel).
- `package-smoke` uses the Docker daemon for clean-distribution containers in
  both environments; local default-backend runs dispatch it to host Docker.
- GitHub's `sudo` configuration differs from the image's (`ci` has passwordless
  sudo in CI images; scripts use sudo only for the steps that need it).

## Unit-test isolation and the production-path guard

Unit tests must never touch production paths (`/etc/veil`, `/var/lib/veil`,
`/usr/local/bin`, `/run/veil`). This is enforced, not aspirational:

- `internal/testguard` exposes a zero-cost hook that `internal/api`'s
  `TestMain` arms to **panic** with
  `unit test attempted to use production path: <path>` whenever code falls
  back to a production default location. Production binaries never arm it.
- `newManagementState` no longer calls `os.Setenv(VEIL_STATE_PATH/VEIL_KEY_PATH)`:
  those process-global side effects leaked paths between tests (order- and
  shuffle-dependent failures) and, worse, let a test process running as root
  operate on the live system state.
- A `ServerInfo` with neither `StatePath` nor `KeyPath` now gets an ephemeral
  per-instance key under `os.MkdirTemp`, so bare `ServerInfo{Mode: "dev"}`
  constructions in tests are hermetic by construction. Production `veil serve`
  always passes explicit paths resolved from flags/`VEIL_*` env.
- Tests that exercise the env-fallback catalog call `isolateCatalogEnv(t)`.

**Never run the Go test suite as root on a machine with a live Veil install.**
Before the guard existed, a root test run on the live host overwrote
`/usr/local/bin/veil` with a test fixture and restored test fixtures over the
live `/etc/veil/state.key` + `/var/lib/veil/state.json`, taking the panel,
helper, and protocol units down. The recovery path (pre-restore backups under
`/etc/veil/*.pre-restore-*`) worked, but the correct fix is the isolation
above. All authoritative runs happen as the unprivileged `ci` user inside the
VM/container exactly to make this class of accident impossible.
