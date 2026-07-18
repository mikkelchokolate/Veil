# Contributing to Veil

Thanks for helping improve Veil. This document covers the local development
workflow, quality gates, and conventions the project expects.

## Requirements

- **Go 1.26+** (see `go.mod` — currently `go 1.26.5`).
- **Node.js 20+** only for the Playwright browser suite in `test/browser/`.
  The Panel itself is generated from Go (no separate frontend build).
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
