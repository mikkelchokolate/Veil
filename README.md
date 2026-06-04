# Veil

Veil is a management panel for NaiveProxy, Hysteria2, olcRTC, and Mieru. It installs the Panel first; proxy Inbounds are added later from the Panel.

![Veil Panel Dashboard](veil_panel_dashboard.png)

## Quick start

One command, answer a few questions, done:

```bash
curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | sudo bash
```

The installer asks for the Panel exposure mode (`local`, `direct`, or `caddy`)
and whether to choose a random high Panel port or use a port you enter. Veil
still installs as a **panel-only** control plane first; you do not need a domain
or Caddy unless you choose Caddy Panel access or add a NaiveProxy Inbound later.

After install you'll see either direct/local Panel access over generated self-signed HTTPS:

```text
Panel access: https://127.0.0.1:<panel-port>/
```

or, if you choose Caddy Panel access:

```text
Panel URL: https://vpn.example.com/a1b2c3d4e5f6/
```

The HTTPS Panel URL path is randomly generated — only you know it. Direct/local Panel access uses a self-signed certificate, so the browser may ask you to trust it.

### Non-interactive examples

Panel-only local access:

```bash
curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | sudo bash -s -- \
  --panel-access local \
  --yes
```

Panel HTTPS access through Caddy on a domain, without exposing the Panel port:

```bash
curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | sudo bash -s -- \
  --panel-access caddy \
  --domain vpn.example.com \
  --email admin@example.com \
  --yes
```

Protocol runtimes are not selected during install. Open the Panel after install and add NaiveProxy, Hysteria2, olcRTC, or Mieru as Inbounds.

### First-run admin setup

Veil expects Panel user/session authentication before any public exposure. For local installs, the first server start can generate one-time admin credentials and print them to the service log. For planned public exposure, initialize credentials explicitly before switching the listener to a public interface:

```bash
sudo veil admin reset
# or set your own account
sudo veil admin set --username admin --password 'use-a-long-random-password' --role admin
```

When `veil serve` listens on a non-loopback address, it refuses to start unless both an API token and at least one Panel user are configured.

## Inbounds

Open the Panel and add Inbounds for the protocols you need:

| Protocol | Transports | Notes |
|---|---:|---|
| NaiveProxy | TCP | Requires Caddy, domain, email, username, and password |
| Hysteria2 | UDP | Does not require Caddy |
| olcRTC | UDP | Does not require Caddy |
| Mieru | TCP or UDP | Does not require Caddy |

TCP and UDP can use the same numeric port at the same time. For example, `mieru tcp 443` and `mieru udp 443` are different transport bindings and can coexist.

Passwords are auto-generated when omitted. You can replace them in the Panel.

## What you get

After panel-only install, Veil runs:

| Service | What |
|---|---|
| `veil.service` | Web Panel + API |

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
veil uninstall --yes
```

### Panel UX controls

The Panel includes operator-focused controls for production use:

- **Client exports** - copy client links, download JSON/subscription exports, and render QR codes locally through Veil.
- **Users and sessions** - create admin/viewer users, audit active browser sessions, and revoke stale sessions.
- **API token rotation helper** - generate a replacement token in the UI, then update `VEIL_API_TOKEN` or `--auth-token` and restart the Panel.
- **Safe apply preview** - review generated files, staged/live paths, backups, rollback files, runtime actions, and DNS/TLS/firewall/service warnings before applying.
- **Viewer role** - read-only users can inspect status, diagnostics, logs, client exports, and previews while mutation controls require admin.

### Backup, rollback, and audit

Repair can write backups and a JSONL audit log. The backup directory must be writable; audit entries are not written during `--dry-run`.

```bash
veil repair --backup-dir /var/lib/veil/backups --audit-log /var/log/veil/audit.jsonl --yes
veil rollback list --backup-dir /var/lib/veil/backups
veil rollback restore <backup-id> --backup-dir /var/lib/veil/backups --yes
veil rollback cleanup <backup-id> --backup-dir /var/lib/veil/backups --yes
```

## Security

- **Fail-closed public listen** - non-loopback `veil serve` requires both `VEIL_API_TOKEN`/`--auth-token` and user/session auth
- **API token** - accepted as `X-Veil-Token` or `Authorization: Bearer`
- **Session auth** - `/api/auth/login` issues an HTTP-only session cookie; mutating cookie requests require CSRF and admin role
- **Metrics policy** - `/metrics` has a separate `--metrics-access` / `VEIL_METRICS_ACCESS` policy and cannot be public on a public Panel listener
- **HTTPS Panel access** — generated self-signed Panel TLS without Caddy, or Caddy with random Web base path
- **Encryption** — secrets encrypted with AES-256-GCM (`/etc/veil/state.key`)
- **TLS 1.2+** — when HTTPS is enabled
- **Rate limiting** — protects expensive endpoints
- **Input validation** — all API inputs validated

See the [hardening guide](docs/HARDENING.md) for deployment hardening, supply-chain verification (signed releases, SBOM), and systemd hardening.

### Safe exposure modes

| Mode | Listener | Required auth | Notes |
|---|---|---|---|
| Local + SSH tunnel | `127.0.0.1:<panel-port>` | Session user recommended; token optional | Recommended and safest. Use `ssh -L <panel-port>:127.0.0.1:<panel-port> host`. |
| Caddy Panel access | Veil on loopback, Caddy on `443` | Session user; token for API clients | Recommended public mode. Caddy terminates HTTPS and routes a random Web base path. |
| Direct public listen | `0.0.0.0:2096` or public IP | Session user and API token | `veil serve` refuses to start without both. Use TLS and authenticated metrics. |

## Documentation

- [Installation guide](docs/install.md) — setup options, access modes, and compiling from source
- [Troubleshooting guide](docs/troubleshooting.md) — diagnostics, logs, and state rollback
- [Hardening guide](docs/HARDENING.md) — secure deployment and operations
- [Known limitations](docs/known-limitations.md) — multi-inbound behavior and platform limits
- [API reference (OpenAPI)](docs/openapi.yaml) — the Panel HTTP management API
- [Security policy](SECURITY.md) — vulnerability reporting
- [Changelog](CHANGELOG.md) — release history
- [Context](CONTEXT.md) — domain language and architecture

## Native packages

Prebuilt `.deb`, `.rpm`, and `.apk` packages are attached to each [release](https://github.com/mikkelchokolate/Veil/releases) for linux amd64/arm64. They install the `veil` binary and managed systemd units; run `veil install` afterward to configure Panel access and credentials. Build locally with `make package` (requires [nfpm](https://nfpm.goreleaser.com)).

### Verify a release

Each release ships checksums, an SPDX SBOM, keyless cosign signatures, and GitHub provenance attestations. Before installing manually:

```bash
cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
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

The end-to-end suite (`test/e2e/`, guarded by the `e2e` build tag) compiles the `veil` binary, runs `veil serve` as a subprocess bound to a real port, and drives it over HTTP — covering the readiness lifecycle, API auth gating, graceful shutdown, state persistence across restarts, the full inbound-to-apply flow, and the CLI subcommands.
