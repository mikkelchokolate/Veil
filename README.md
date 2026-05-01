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
- NaiveProxy client URL
- Hysteria2 client URI
- generated Caddyfile
- generated Hysteria2 server.yaml

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
- /var/lib/veil/www/index.html
- optional /etc/systemd/system/veil.service
- optional /etc/systemd/system/veil-naive.service
- optional /etc/systemd/system/veil-hysteria2.service

Panel port behavior:

- `--panel-port 0` selects a random high port
- `--panel-port 2096` uses the user-selected port
- `--interactive` asks whether to customize the panel port; no means random
- future curl installer will call the same interactive flow, following the 3x-ui installer pattern

## Roadmap

Next milestones:

1. Download and verify Caddy/NaiveProxy and Hysteria2 binaries.
2. Wire safe systemd plan execution after binary/config validation.
3. Add config validation before restart.
4. Add authentication and persistence for the web panel.
5. Add WARP outbound management.
6. Add routing rule editor.
