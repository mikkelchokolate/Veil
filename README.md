# Veil

Veil is an open-source management panel and CLI for NaiveProxy and Hysteria2.

It is designed as a control plane: Veil manages installation, configuration, panel/API state, routing, WARP settings, and safe apply workflows while keeping NaiveProxy/Caddy, Hysteria2, and sing-box as separate services.

Core idea:

- NaiveProxy runs on TCP, for example TCP/443.
- Hysteria2 runs on UDP, for example UDP/443.
- Both can share the same numeric port because TCP and UDP port spaces are separate.
- You can install both together, or only NaiveProxy, or only Hysteria2.

Status: early development. Do not use on production servers yet.

## Quick start

Install the latest Linux release:

```bash
curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | bash
```

Recommended RU profile with explicit shared proxy port:

```bash
curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | bash -s -- \
  --profile ru-recommended \
  --domain example.com \
  --email admin@example.com \
  --port 443 \
  --stack both
```

Build from source:

```bash
make test
make build
./bin/veil version
```

Dry run example:

```bash
./bin/veil install \
  --profile ru-recommended \
  --domain example.com \
  --email admin@example.com \
  --port 443 \
  --stack both \
  --dry-run
```

Notes:

- `--port` is the shared proxy port. Veil does not silently choose 443, 8443, or a random port for you.
- With `--stack both`, NaiveProxy uses TCP on the selected port and Hysteria2 uses UDP on the same numeric port.
- The panel runs on a separate TCP port; use `--panel-port` to choose it explicitly.
- The curl installer downloads a GitHub Release archive, verifies it with `checksums.txt`, installs `veil`, then runs `veil install`.

## Roadmap

- Finish the first production-ready installer flow.
- Expand the web panel for settings, inbounds, routing rules, WARP, apply history, and service status.
- Add safer update and repair workflows.
- Publish the first tagged prerelease with Linux amd64/arm64 binaries.
- Continue hardening validation, rollback, audit logs, and secret redaction.
