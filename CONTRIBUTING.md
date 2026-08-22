# Contributing to Veil

Thanks for helping improve Veil. This document covers the local development
workflow, quality gates, and conventions the project expects.

## Requirements

- **Go 1.27+** (see `go.mod` — currently `go 1.27.0`).
- **Node.js 26.7.0** (repository pin) for the React Panel build and Playwright/browser suites; Node.js 20 is not supported by the project CI contract.
- **A Linux host with systemd** for the integration and end-to-end suites;
  bare-metal root/systemd access for the full `make e2e` path.

## Development loop

1. Write a failing test first (the project follows TDD).
2. Implement the minimal change to make it pass.
3. Run the relevant verification before opening a PR:
   ```bash
   make build    # compile the binary
   make test     # unit + in-process integration tests
   ```
4. Keep PRs small and focused; one concern per change.

## Verification targets

| Target | What it runs |
|---|---|
| `make build` | Compiles `bin/veil` from `./cmd/veil`. |
| `make test` | Unit + in-process integration tests across `internal/...`. |
| `make e2e` | End-to-end suite (`test/e2e/`, guarded by the `e2e` build tag): compiles the binary, runs `veil serve` as a subprocess on a real port, and drives it over HTTP. Requires a systemd host and root privileges; do **not** run it on a live production host. |
| `make generate-sdk` | Regenerates the Go client in `sdk/go` from `docs/openapi.yaml`. |
| `make verify-sdk` | Contract tests + Redocly validation of the generated client. |
| `make verify-openapi` | Lints `docs/openapi.yaml`. |
| `make package` | Builds `.deb`, `.rpm`, and `.apk` packages via nfpm. |
| `make release-check` / `make verify-release` | Full pre-tag gate: tests, e2e, shell validation, build, and OpenAPI lint. |

## Local CI (pre-push gates)

`go test ./...` is not what GitHub Actions runs. The CI gates below execute
the same scripts (`scripts/ci/*.sh`) on the same Ubuntu 24.04 user-space as
the workflows — inside a hardware-isolated microVM via
[smolvm](https://github.com/smol-machines/smolvm). See
[docs/development/ci.md](docs/development/ci.md) for setup (KVM / WHP),
architecture and troubleshooting.

Before pushing, run `make ci`.

Before opening or updating a pull request, run `make ci-pr`.

| Target | What it runs |
|---|---|
| `make ci-fast` | Pre-commit quick checks on the host (tidy/gofmt/vet, drift, frontend checks, fast unit tests). **Not** a full CI reproduction. |
| `make ci` | Pre-push gate in a VM: frontend, test (`-race -count=1`, coverage threshold), lint, image build validation. |
| `make ci-full` | Every job: adds browser E2E, privilege boundary, real protocol E2E, package smoke, systemd lifecycle. |
| `make ci-pr` | `ci-full` on a temporary merge of HEAD with `origin/main` (never touches your branch or pushes). |
| `make ci-job JOB=<job>` | One CI job in a VM. |
| `make ci-stress` | Race/shuffle stress runs for historically flaky tests. |
| `make ci-host` / `make ci-job-host` | Diagnostic host execution (non-authoritative, warns loudly). |


When you change the HTTP management API, update `docs/openapi.yaml` and
re-run `make generate-sdk` + `make verify-sdk` so the typed client stays in
sync with the contract.

## Code conventions

- The codebase is organised into small, focused packages under `internal/`.
  Keep HTTP routes and Cobra commands as thin Adapters over package logic —
  see `CONTEXT.md` for the domain language and Module/Adapter boundaries.
- Match the surrounding style; keep changes KISS/DRY.
- Secrets are redacted and encrypted at rest (`internal/secrets`); never log
  or persist raw passwords, tokens, cookies, or CSRF values.
- Proxy protocols are plugins under `internal/protocols/<protocol>`; add new
  protocol behavior through a plugin rather than scattering conditionals.
