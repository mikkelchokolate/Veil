# Veil

Veil is an open-source management panel and CLI for NaiveProxy and Hysteria2.

The core feature is running NaiveProxy on TCP and Hysteria2 on UDP using the same numeric port, for example:

- TCP/443: NaiveProxy via Caddy forward_proxy
- UDP/443: Hysteria2

Status: early development skeleton. Do not use on production servers yet.

## Current capabilities

- Go CLI binary named `veil`
- Transport-aware shared-port planning
- 443 -> 8443 -> random high port fallback
- RU recommended profile builder
- Stack selection: `--stack both`, `--stack naive`, or `--stack hysteria2`
- NaiveProxy Caddyfile renderer
- Hysteria2 server.yaml renderer
- Generated fallback website
- Safe atomic writes for generated config files
- Optional systemd unit rendering/writing
- Initial HTTP API and embedded panel shell
- Panel speedtest action via `speedtest-cli` or Ookla `speedtest`
- Initial API sections for settings, inbounds, routing rules, and WARP
- File-backed management state persistence for panel settings/inbounds/routing/WARP
- Apply-plan API and panel control to validate state before staged config/reload work
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
  --stack both \
  --dry-run
```

The dry run prints:

- selected shared port
- NaiveProxy client URL with generated password redacted
- Hysteria2 client URI with generated password redacted
- generated Caddyfile with generated password redacted
- generated Hysteria2 server.yaml with generated password redacted

## Local apply test

This writes generated files into custom directories instead of system paths:

```bash
./bin/veil install \
  --profile ru-recommended \
  --domain example.com \
  --email admin@example.com \
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

Panel port behavior:

- `--panel-port 0` selects a random high port
- `--panel-port 2096` uses the user-selected port
- `--interactive` asks whether to customize the panel port; no means random
- future curl installer will call the same interactive flow, following the 3x-ui installer pattern

Panel API auth:

- `veil serve --auth-token ...` protects `/api/*` with bearer-token auth.
- `VEIL_API_TOKEN=... veil serve` is the environment-file friendly form.
- `veil serve --state /path/to/state.json` controls where settings/inbounds/routing/WARP state is persisted.
- `VEIL_STATE_PATH=/path/to/state.json veil serve` is the environment-file friendly state path form.
- The default state path is `/var/lib/veil/state.json`.
- The generated `veil.service` reads `/etc/veil/veil.env` when present.
- `/healthz` remains public for service health checks.
- The embedded panel can store the token in browser `localStorage` and sends it as `X-Veil-Token`.

## Roadmap

Next milestones:

1. Download and verify Caddy/NaiveProxy and Hysteria2 binaries.
2. Wire safe systemd plan execution after binary/config validation.
3. Add config validation before restart.
4. Wire management changes into config rendering and safe service reloads.
5. Add WARP outbound implementation.
6. Add richer routing rule editor.
