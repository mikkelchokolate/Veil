# AGENTS.md

## Cursor Cloud specific instructions

Veil is a single Go binary (`veil`) that bundles a CLI plus an HTTP API and an
embedded React SPA (the "Panel"). There is no external database — state is an
AES-encrypted JSON file. The repo has two parts: the Go backend (repo root) and
the React frontend in `web/`.

### Toolchain / versions (already provisioned in the base environment)

- Go `1.27.0` (matches `go.mod`; `scripts/ci/versions.sh` is the source of truth).
- Node `26.7.0` and pnpm `11.22.0` are the pinned frontend versions
  (`web/package.json` `packageManager`, Dockerfile, and `scripts/ci/versions.sh`).
  Node 26.7.0 is installed via `nvm`. The runner injects a different Node
  (`/exec-daemon/node`, currently v22.x) at the front of `PATH`, so `node`
  resolves to that by default. Before running any frontend command, activate the
  pinned Node for the shell:

  ```bash
  export NVM_DIR="$HOME/.nvm"
  export PATH="$HOME/.nvm/versions/node/v26.7.0/bin:$PATH"   # or: nvm use 26.7.0
  ```

  Use `corepack pnpm ...` so the pinned pnpm 11.22.0 (from `packageManager`) is
  used rather than any globally installed pnpm.
- `caddy` v2.11.4 built with the `klzgrad/forwardproxy` module is installed at
  `/usr/local/bin/caddy`. It is required for the `internal/protocols` and
  `internal/protocols/naiveproxy` Go tests (they probe `caddy list-modules
  --json`). Without it those two packages fail; the rest of `go test ./...`
  passes without any protocol runtimes.

### Build / lint / test / run

Commands are defined in the `Makefile` and `web/package.json` — prefer those.

- Frontend (run from `web/`, with pinned Node active): `corepack pnpm install
  --frozen-lockfile`, then `corepack pnpm build` (outputs `web/dist`),
  `corepack pnpm typecheck`, `corepack pnpm check` (Biome lint),
  `corepack pnpm test` (Vitest jsdom), `corepack pnpm gen` (regenerate API
  client; CI checks `src/api/generated` for drift), `corepack pnpm i18n:check`.
  The full frontend CI job is `scripts/ci/frontend.sh`.
- Backend: `make build` (=> `bin/veil`), `go vet ./...`, `go test ./...`.
  IMPORTANT: `web/web.go` embeds `web/dist` via `//go:embed all:dist`, so the
  frontend MUST be built (`corepack pnpm build` or `make web`) before the Go
  binary will compile.
- Run the Panel: `./bin/veil serve --listen 127.0.0.1:2096`. On a loopback
  listener with no configured admin, the Panel exposes a first-run setup page to
  create the initial administrator. Non-loopback listeners refuse to start
  unless an API token, a Panel user, authenticated metrics, and TLS are all
  configured (see README "First-run admin setup").

### Notes / gotchas

- `docker compose up -d` runs the production image; for development, run the
  locally built `bin/veil` directly instead.
- Protocol runtimes (hysteria, mita/mieru, naive, sing-box) and Caddy are
  optional integrations Veil installs/manages; only `caddy`-with-forwardproxy is
  needed for the unit test suite (see above). The `test/e2e` and
  `test/linuxintegration` suites are Linux/systemd-oriented and provision pinned
  runtimes via `scripts/ci/runtimes.sh`.
