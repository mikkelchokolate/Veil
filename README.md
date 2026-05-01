# Veil

Veil is an open-source management panel and CLI for NaiveProxy and Hysteria2.

The core feature is running NaiveProxy on TCP and Hysteria2 on UDP using the same numeric port, for example:

- TCP/443: NaiveProxy via Caddy forward_proxy
- UDP/443: Hysteria2

Status: early development skeleton. Do not use on production servers yet.

## Current capabilities

- Go CLI binary named `veil`
- Transport-aware shared-port planning
- Installation CLI requires an explicit user-selected shared proxy port via `--port`
- RU recommended profile builder
- Stack selection: `--stack both`, `--stack naive`, or `--stack hysteria2`
- NaiveProxy Caddyfile renderer
- Hysteria2 server.yaml renderer
- Generated fallback website
- Safe atomic writes for generated config files
- Hysteria2 release asset planning with an explicit sha256 requirement and verified atomic binary downloader
- Optional systemd unit rendering/writing
- Initial HTTP API and embedded panel shell
- Panel speedtest action via `speedtest-cli` or Ookla `speedtest`
- Initial API sections for settings, inbounds, routing rules, and WARP
- File-backed management state persistence for panel settings/inbounds/routing/WARP
- WARP outbound sidecar rendering via sing-box WireGuard config with a local SOCKS inbound for proxy chaining
- Routing-rule validation for supported outbounds (`direct` and enabled `warp`)
- Apply-plan API and panel control to validate state before staged config/reload work
- Confirmed staged apply API writes plan/state artifacts, stages rendered Caddy/Hysteria2/sing-box WARP candidate configs from management state, reports fixed-command syntax validation results, can explicitly promote validated configs to live files with backups, can explicitly run allowlisted service reloads with health checks and rollback, and records file-backed apply history
- Optional token protection for `/api/*` via `--auth-token` or `VEIL_API_TOKEN`
- Unit tests and GitHub Actions CI

## Build

```bash
make test
make build
./bin/veil version
```

## RU recommended dry run

```bash
./bin/veil install \
  --profile ru-recommended \
  --domain example.com \
  --email admin@example.com \
  --port 443 \
  --stack both \
  --dry-run
```

The dry-run output includes:

- user-selected shared proxy port from `--port` (for `both`, NaiveProxy uses TCP and Hysteria2 uses UDP on the same numeric port)
- NaiveProxy client URL with generated password redacted
- Hysteria2 client URI with generated password redacted
- generated Caddyfile with generated password redacted
- generated Hysteria2 server.yaml with generated password redacted
- Hysteria2 release asset URL and install path
- sha256 status: either the supplied `--hysteria-sha256` value or `required before binary download`

Binary acquisition is intentionally checksum-first. Veil will not download or replace a binary unless a sha256 checksum is supplied for the requested asset. Verified downloads write a temporary file, chmod it, and atomically rename it into place only after the checksum matches.

## Local apply test

This writes generated files into custom directories instead of system paths:

```bash
./bin/veil install \
  --profile ru-recommended \
  --domain example.com \
  --email admin@example.com \
  --port 443 \
  --etc-dir /tmp/veil/etc \
  --var-dir /tmp/veil/var \
  --systemd-dir /tmp/veil/systemd \
  --panel-port 0 \
  --yes
```

Default production paths will be:

- /etc/veil/generated/caddy/Caddyfile
- /etc/veil/generated/hysteria2/server.yaml
- /etc/veil/veil.env with `VEIL_API_TOKEN` for the panel service
- /var/lib/veil/state.json for management API persistence
- /var/lib/veil/www/index.html
- optional /etc/systemd/system/veil.service
- optional /etc/systemd/system/veil-naive.service
- optional /etc/systemd/system/veil-hysteria2.service
- optional /etc/systemd/system/veil-warp.service for the sing-box WARP sidecar

Panel port behavior:

- `--port <1-65535>` is required for installation/repair and is the shared proxy port; Veil no longer auto-falls back from 443 to 8443/random in the installer CLI
- `--interactive` prompts for the shared proxy port if it was not supplied
- `--panel-port 0` selects a random high port
- `--panel-port 2096` uses the user-selected port
- `--interactive` asks whether to customize the panel port; no means random
- future curl installer will call the same interactive flow, following the 3x-ui installer pattern

## Repair dry run

```bash
./bin/veil repair \
  --profile ru-recommended \
  --domain example.com \
  --email admin@example.com \
  --port 443 \
  --dry-run
```

`veil repair` compares the expected managed files with the current files and reports only missing or drifted files. Applying repair still requires `--yes`; it rewrites only planned managed files and does not run arbitrary service commands.

Panel API auth:

- `veil serve --auth-token ...` protects `/api/*` with bearer-token auth.
- `VEIL_API_TOKEN=... veil serve` is the environment-file friendly form.
- `veil serve --state /path/to/state.json` controls where settings/inbounds/routing/WARP state is persisted.
- `VEIL_STATE_PATH=/path/to/state.json veil serve` is the environment-file friendly state path form.
- The default state path is `/var/lib/veil/state.json`.
- `veil serve --apply-root /path/to/root` controls where confirmed staged apply artifacts are written.
- `VEIL_APPLY_ROOT=/path/to/root veil serve` is the environment-file friendly apply root form.
- The default apply root is `/etc/veil`; staged plan/state artifacts are written under `generated/veil/`.
- When management settings include render inputs (`domain`, `email`, `naiveUsername`, `naivePassword`, `hysteria2Password`, optional `masqueradeURL`/`fallbackRoot`), confirmed staged apply also writes candidate configs under `<apply-root>/generated/caddy/Caddyfile` and `<apply-root>/generated/hysteria2/server.yaml` according to the selected stack.
- When `/api/warp` is enabled with WireGuard fields (`privateKey`, `localAddress`, `peerPublicKey`, optional `reserved`, `socksListen`, `socksPort`, `mtu`), confirmed staged apply writes a sing-box WARP sidecar config at `<apply-root>/generated/sing-box/warp.json`. API responses redact `privateKey` and `licenseKey` as `[REDACTED]`.
- Enabled routing rules support `direct` and `warp` outbounds. A rule using `warp` is rejected by `/api/apply/plan` unless WARP is enabled and renderable.
- After staging candidate configs, Veil reports syntax validation results using fixed server-side commands only: `caddy validate --config <candidate>`, `hysteria server --config <candidate> --check`, and `sing-box check -c <candidate>`. Missing binaries are reported as skipped validations.
- `POST /api/apply` remains staged-only by default with `{ "confirm": true }`. Passing `{ "confirm": true, "applyLive": true }` additionally promotes only successfully validated candidate configs into `<apply-root>/live/caddy/Caddyfile`, `<apply-root>/live/hysteria2/server.yaml`, and `<apply-root>/live/sing-box/warp.json`, backing up any replaced files under `<apply-root>/backups/`. Failed or skipped validation prevents live promotion.
- Passing `{ "confirm": true, "applyLive": true, "applyServices": true }` additionally runs fixed allowlisted service reloads only after live promotion succeeds: `systemctl reload veil-naive.service`, `systemctl reload veil-hysteria2.service`, and/or `systemctl reload veil-warp.service` according to the promoted live configs. After reload, Veil runs fixed health checks with `systemctl is-active --quiet <service>`. If reload or health fails, Veil restores promoted live config files from the backup set and reloads the affected services again. Arbitrary commands are not accepted.
- Successful staged/live/service applies and failed validation/rollback outcomes are appended newest-first to `<apply-root>/generated/veil/apply-history.json`; `GET /api/apply/history` returns that audit history for the panel without exposing proxy secrets.
- `GET/PUT /api/settings` redact proxy passwords in API responses as `[REDACTED]`; persisted state and staged/live config files keep the real values with restrictive file permissions so rendering can work.
- The generated `veil.service` reads `/etc/veil/veil.env` when present.
- `/healthz` remains public for service health checks.
- The embedded panel can store the token in browser `localStorage` and sends it as `X-Veil-Token`.

## Roadmap

Next milestones:

1. Add richer routing rule editor.
2. Add release/curl installer workflow.
