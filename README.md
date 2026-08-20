# Veil

Veil is a management panel for NaiveProxy, Hysteria2, olcRTC, and Mieru. It installs the Panel first; proxy Inbounds are added later from the Panel.

![Veil panel](docs/assets/veil-panel.gif)

## Quick start

One command, answer a few questions, done:

```bash
curl -fsSL https://github.com/mikkelchokolate/Veil/releases/latest/download/install.sh | sh
```

`install.sh` runs without root. It downloads and verifies the pinned verifier,
`install-privileged.sh`, the Veil archive, signed checksums, and signed
provenance. Only after every privileged payload passes verification does it
invoke `sudo` itself.

The installer asks for the Panel exposure mode (`local`, `direct`, or `caddy`)
and whether to choose a random high Panel port or use a port you enter. Veil
still installs as a **panel-only** control plane first; you do not need a domain
or Caddy unless you choose Caddy Panel access or add a NaiveProxy Inbound later.

After install you'll see either direct/local Panel access over generated self-signed HTTPS:

```text
Panel access: https://127.0.0.1:<panel-port>/<web-base-path>/
```

or, if you choose Caddy Panel access:

```text
Panel URL: https://vpn.example.com/a1b2c3d4e5f6/
```

The HTTPS Panel URL path is randomly generated — only you know it. Direct/local Panel access uses a self-signed certificate, so the browser may ask you to trust it.

### Non-interactive examples

Panel-only local access:

```bash
curl -fsSL https://github.com/mikkelchokolate/Veil/releases/latest/download/install.sh | sh -s -- \
  --panel-access local \
  --yes
```

Panel HTTPS access through Caddy on a domain, without exposing the Panel port:

```bash
curl -fsSL https://github.com/mikkelchokolate/Veil/releases/latest/download/install.sh | sh -s -- \
  --panel-access caddy \
  --domain vpn.example.com \
  --email admin@example.com \
  --yes
```

Protocol runtimes are not selected during install. Open the Panel after install and add NaiveProxy, Hysteria2, olcRTC, or Mieru as Inbounds.

### First-run admin setup

Veil expects Panel user/session authentication before any public exposure. An
unconfigured `local` install opens a loopback-only first-run page where you
create the initial administrator and confirm backup responsibilities. The
setup API is disabled for `direct`, `caddy`, and non-loopback listeners.

For planned public exposure, initialize credentials before changing the access
mode:

```bash
sudo veil admin reset
# or set your own account
sudo veil admin set --username admin --password 'use-a-long-random-password' --role admin
```

When `veil serve` listens on a non-loopback address, it refuses to start unless
an API token, at least one Panel user, authenticated metrics, and TLS are
configured. Caddy exposure also requires a Panel user and authenticated metrics
before the reverse proxy is allowed to expose the loopback listener.

## Inbounds

Open the Panel and add Inbounds for the protocols you need:

| Protocol | Transports | Notes |
|---|---:|---|
| NaiveProxy | TCP | Requires Caddy, domain, email, username, and password |
| Hysteria2 | UDP | Does not require Caddy; real ACME cert when given a domain |
| olcRTC | UDP | Does not require Caddy |
| Mieru | TCP or UDP | Does not require Caddy |

TCP and UDP can use the same numeric port at the same time. For example, `mieru tcp 443` and `mieru udp 443` are different transport bindings and can coexist.

Passwords are auto-generated when omitted. You can replace them in the Panel.

### Hysteria2 TLS certificates

Hysteria2 speaks QUIC and needs a TLS certificate for its SNI. When you assign a
domain to a Hysteria2 Inbound, Veil obtains a real ACME certificate for it
instead of a self-signed one, so clients connect without `insecure`:

- **Domain already used by the Panel or a NaiveProxy Inbound** — the Hysteria2
  Inbound reuses the certificate Caddy already manages for that domain
  (`tls-alpn-01`). No extra port is needed.
- **A Hysteria2-only domain** (any domain that resolves to the host) — Veil
  switches it to the HTTP-01 challenge and adds a dedicated ACME challenge
  listener on TCP `:80`, then syncs the issued certificate to the Inbound.
  Requires TCP `:80` free (or already served by Caddy) and DNS pointing to the
  host. If `:80` is unavailable the apply still succeeds with a warning, and
  the Inbound runs on a self-signed certificate until `:80` is freed.
- **No domain** — the Inbound uses a self-signed certificate and clients use
  `insecure` (previous behavior, unchanged).

## What you get

After panel-only install, Veil runs:

| Service | What |
|---|---|
| `veil.service` | Web Panel + API (runs as the locked `veil` user) |
| `veil-helper.socket` + `veil-helper.service` | Root privileged helper for host mutations the unprivileged Panel cannot perform |

Enabling scheduled backups adds `veil-backup.timer` + `veil-backup.service`
(daily encrypted archives with retention).

When you add and apply Inbounds, Veil can also manage:

| Service | What |
|---|---|
| `veil-caddy@.service` | NaiveProxy via Caddy |
| `veil-hysteria2@.service` | Hysteria2 |
| `veil-olcrtc@.service` | olcRTC |
| `veil-mieru.service` | Mieru |

All secrets are stored encrypted at rest.

## Manage

```bash
veil status
veil version --check
veil update --yes --staged
veil uninstall --yes              # also removes config/state; reinstall starts fresh
veil uninstall --yes --keep-data  # preserve credentials and config across a reinstall
```

All CLI commands:

| Command | Purpose |
|---|---|
| `veil install` | Install and configure Panel access, credentials, and runtimes. |
| `veil serve` | Run the HTTP API and Web Panel. |
| `veil status` | Show managed service status (`--json` for machine output). |
| `veil doctor` | Run host readiness checks (required/optional commands). |
| `veil admin` | Manage admin accounts (`reset`, `set`, `show`, `rotate-key`). |
| `veil config validate` | Validate a Management state file without starting a server. |
| `veil runtime install` | Download/verify protocol runtime binaries (`--only`, `--bin-dir`). |
| `veil backup` | Encrypted backup lifecycle (`create`, `list`, `verify`, `restore`, `prune`, `schedule`). |
| `veil repair` | Repair managed generated files without arbitrary side effects. |
| `veil rollback` | List/restore/cleanup configuration backups. |
| `veil update` | Download and install the latest Veil release. |
| `veil uninstall` | Remove the Panel, services, configuration, and state. |
| `veil version` | Print the Veil version (`--check` to compare with the latest release). |

### Panel UX controls

The Panel includes operator-focused controls for production use:

- **Client exports** - copy client links, download JSON/subscription exports, and render QR codes locally through Veil.
- **Users and sessions** - create admin/viewer users, inspect durable browser sessions, and revoke stale sessions.
- **English and Russian UI** - choose a language during setup, from the Panel header, or per user; authenticated preferences survive logout and restart.
- **API token rotation helper** - generate a replacement token in the UI, then update `VEIL_API_TOKEN` or `--auth-token` and restart the Panel.
- **Live validation and safe apply preview** - inspect field-level port, DNS, runtime, and unit checks plus structured generated/live file operations, rollback availability, and interruption risk before applying.
- **Viewer role** - read-only users can inspect status, diagnostics, logs, client exports, and previews while mutation controls require admin.
- **Accessible operation** - skip navigation, keyboard-contained dialogs, live status announcements, reduced-motion support, and responsive layouts are included.

### Backup, rollback, and audit

The Panel records authentication, user, configuration, apply, and service
actions in a structured, redacted, rotated JSONL log. With the default state
path it is stored at `/var/lib/veil/audit/panel.jsonl`; administrators can read
the bounded history through `GET /api/audit`.

Encrypted disaster-recovery archives include Management state, its encryption
key, a consistent `veil.db` snapshot, and a checksum manifest. Native packages
ship a daily systemd timer with daily/weekly/monthly retention. The Panel can create, verify,
download, prune, and restore managed archives without sending the backup
passphrase to the browser. The configured backup and audit directories must be
writable by the process performing each operation.

```bash
sudo veil backup create --passphrase-file /root/veil-backup-passphrase \
  --output-dir /var/lib/veil/backups
sudo veil backup schedule enable --passphrase-file /root/veil-backup-passphrase
sudo veil backup list --dir /var/lib/veil/backups
sudo veil backup prune --dir /var/lib/veil/backups \
  --daily 7 --weekly 4 --monthly 12 --dry-run
sudo veil backup verify /var/lib/veil/backups/veil_backup_YYYYMMDD_HHMMSS_NNNNNNNNN_RANDOMHEX.tar.gz.enc \
  --passphrase-file /root/veil-backup-passphrase
sudo veil backup restore /path/to/archive.tar.gz.enc \
  --passphrase-file /root/veil-backup-passphrase --check-only

veil repair --backup-dir /var/lib/veil/backups --audit-log /var/log/veil/audit.jsonl --yes
veil rollback list --backup-dir /var/lib/veil/backups
veil rollback restore <backup-id> --backup-dir /var/lib/veil/backups --yes
veil rollback cleanup <backup-id> --backup-dir /var/lib/veil/backups --yes
```

See the [disaster recovery guide](docs/disaster-recovery.md) for cross-host
restore, safety copies, key rotation, and recovery-drill procedures.

## Security

- **Fail-closed public exposure** - direct listeners require TLS, `VEIL_API_TOKEN`/`--auth-token`, user/session auth, and authenticated metrics; Caddy exposure requires user/session auth and authenticated metrics
- **API token** - accepted as `X-Veil-Token` or `Authorization: Bearer`
- **Session auth** - `/api/auth/login` issues an HTTP-only session cookie; mutating cookie requests require CSRF and admin role. Hashed session state survives Panel restarts, expires after 30 minutes idle or 24 hours absolute, and is revoked when user authority changes
- **Locale preference** - authenticated users, including viewers, may update only their own `en`/`ru` preference through `POST /api/auth/locale`; static API tokens cannot call it
- **Audit history** - security-sensitive Panel actions are redacted and rotated; raw passwords, cookies, tokens, and CSRF values are never written
- **Metrics policy** - `/metrics` has a separate `--metrics-access` / `VEIL_METRICS_ACCESS` policy and cannot be public on a public Panel listener
- **Privilege separation** - the Panel runs as the locked `veil` user with no capabilities; root-only operations use the allowlisted, peer-credential-checked `veil-helper.socket`
- **Direct public listen** — in `direct` mode Veil issues a trusted Let's Encrypt **IP certificate** (SAN = public IP, `shortlived` 3-day profile, auto-renewed via `acme.sh`) so the Panel serves valid HTTPS by IP without a domain
- **Hysteria2 Inbound TLS** — a Hysteria2 Inbound assigned a domain uses a real ACME certificate: it reuses the Caddy-managed cert when the domain is the Panel's or a NaiveProxy Inbound's, and gets a dedicated HTTP-01 challenge on TCP `:80` for a Hysteria2-only domain
- **Encryption** — secrets encrypted with AES-256-GCM (`/etc/veil/state.key`)
- **TLS 1.2+** — when HTTPS is enabled
- **Rate limiting** — protects expensive endpoints
- **Input validation** — all API inputs validated

See the [hardening guide](docs/HARDENING.md) for deployment hardening, supply-chain verification (signed releases, SBOM), and systemd hardening.

### Safe exposure modes

| Mode | Listener | Required auth | Notes |
|---|---|---|---|
| Local + SSH tunnel | `127.0.0.1:<panel-port>` | Session user recommended; token optional | Recommended and safest. Use `ssh -L <panel-port>:127.0.0.1:<panel-port> host`. |
| Caddy Panel access | Veil on loopback, Caddy on `443` | Session user; token for API clients | Recommended public mode. Caddy terminates HTTPS, routes a random Web base path, and forces authenticated metrics. |
| Direct public listen | `0.0.0.0:<panel-port>` or public IP | Session user and API token | `veil serve` refuses to start without TLS, both auth layers, and authenticated metrics. In `direct` mode Veil issues a trusted Let's Encrypt IP certificate for the public IP. |

## Documentation

- [Installation guide](docs/install.md) — setup options, access modes, and compiling from source
- [Troubleshooting guide](docs/troubleshooting.md) — diagnostics, logs, and state rollback
- [Hardening guide](docs/HARDENING.md) — secure deployment and operations
- [Panel operations guide](docs/operations.md) - live validation guarantees, race boundaries, and structured apply previews
- [Known limitations](docs/known-limitations.md) — multi-inbound behavior and platform limits
- [API reference (OpenAPI)](docs/openapi.yaml) — the Panel HTTP management API
- [Generated Go SDK](sdk/go) — typed client generated from the OpenAPI contract
- [Security policy](SECURITY.md) — vulnerability reporting
- [Changelog](CHANGELOG.md) — release history
- [Context](CONTEXT.md) — domain language and architecture
- [Disaster recovery guide](docs/disaster-recovery.md) - encrypted backups, retention, restore, and key rotation

## Native packages

Prebuilt `.deb`, `.rpm`, and `.apk` packages are attached to each [release](https://github.com/mikkelchokolate/Veil/releases) for linux amd64/arm64. They install the `veil` binary, create the locked `veil` account, migrate scoped permissions with safety copies, and ship the hardened Panel, `veil-helper.socket`, backup, and runtime units. Run `veil install` afterward to configure Panel access and credentials. Build locally with `make package` (requires [nfpm](https://nfpm.goreleaser.com)).

### Verify a release

Each release ships checksums, an SPDX SBOM, keyless cosign bundles, and GitHub provenance attestations. Before installing manually:

```bash
cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity-regexp 'https://github.com/mikkelchokolate/Veil/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt

sha256sum -c checksums.txt
gh attestation verify dist/veil_linux_amd64.tar.gz \
  --repo mikkelchokolate/Veil
```

Maintainers can run `make verify-release` before tagging to execute tests, e2e checks, shell validation, build validation, and OpenAPI lint.

## Docker

### Docker Compose (Recommended)

Copy `.env.example` to `.env` and set a secure `VEIL_API_TOKEN`.

- **Local Development / Private Exposure (Default):**
  The default `docker-compose.yml` binds the panel only to loopback (`127.0.0.1:2096`), keeping the control plane private:
  ```bash
  docker compose up -d
  ```

- **Production Exposure:**
  If exposing the panel publicly (e.g. via direct binding or reverse proxy), initialize a Panel admin user first, populate `VEIL_API_TOKEN` with a strong token, and configure TLS/HTTPS. The CLI server refuses to start on non-loopback listeners unless both token auth and user/session auth are configured.

### Single Container Run

To run a single container locally on the host network:
```bash
docker run -d --name veil --network host \
  -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
  ghcr.io/mikkelchokolate/veil:latest serve
```

For direct public container exposure, create the first admin account in the mounted state before starting the public listener:

```bash
docker run --rm \
  -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
  ghcr.io/mikkelchokolate/veil:latest admin reset

docker run -d --name veil -p 2096:2096 \
  -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
  -e VEIL_API_TOKEN='use-a-long-random-token' \
  ghcr.io/mikkelchokolate/veil:latest serve --listen 0.0.0.0:2096
```

## Testing

```bash
make test    # unit + in-process integration tests
make e2e     # end-to-end tests: real veil binary launched over a socket
```

The end-to-end suite (`test/e2e/`, guarded by the `e2e` build tag) compiles the `veil` binary, runs `veil serve` as a subprocess bound to a real port, and drives it over HTTP — covering the readiness lifecycle, API auth gating, graceful shutdown, state persistence across restarts, the full inbound-to-apply flow, and the CLI subcommands. It requires a Linux systemd host with root access; do not run it against a live production install.

Before pushing, run `make ci` — it executes the same scripts as GitHub Actions inside a local microVM on a clean Ubuntu 24.04 user-space.

Before opening or updating a pull request, run `make ci-pr` — it validates the temporary merge of your branch with `origin/main` without touching your working copy.

See [docs/development/ci.md](docs/development/ci.md) for the full local-CI guide.

A Playwright browser suite (`test/browser/`) exercises the Panel UI end to end. The frontend and browser suites use the repository-pinned Node.js v26.5.0 (not Node.js 20) and are separate from the Go test suites:

```bash
cd test/browser && npm install && npm test
```
