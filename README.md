# Veil

Veil is a management panel for NaiveProxy and Hysteria2. It installs, configures, and monitors both proxies — they can share the same port (TCP for NaiveProxy, UDP for Hysteria2) or run separately.

## Quick start

One command, answer a few questions, done:

```bash
curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | bash
```

The installer will ask you:

1. **Domain** — your domain pointed to this server (e.g. `vpn.example.com`)
2. **ACME email** — for Let's Encrypt certificate
3. **Shared proxy port** — the port NaiveProxy and Hysteria2 will use (e.g. `443`)
4. **Panel port** — optional, press Enter for a random one

Then it shows the plan and asks `Apply install plan? [y/N]`. Type `y` to confirm.

After install you'll see:

```
Panel URL: https://vpn.example.com/a1b2c3d4e5f6/
```

The panel URL path is randomly generated — only you know it.

### Non-interactive install (for scripts)

```bash
curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | bash -s -- \
  --domain vpn.example.com \
  --email admin@example.com \
  --port 443 \
  --yes
```

## What you get

After install, Veil runs three systemd services:

| Service | What |
|---|---|
| `veil.service` | Web panel + API (random port, random path) |
| `veil-naive.service` | NaiveProxy via Caddy (TCP) |
| `veil-hysteria2.service` | Hysteria2 (UDP) |

All secrets (passwords, tokens) are auto-generated and stored encrypted at rest.

## Manage

### Check status

```bash
veil status
```

### Update Veil

```bash
veil version --check          # see if update is available
veil update --yes --staged    # update with auto-rollback on failure
```

### Repair configs

```bash
veil repair --domain vpn.example.com --email admin@example.com --port 443 --yes
```

### Uninstall

```bash
veil uninstall --yes
```

### Backup and rollback

Veil backs up configs before every repair. You can list, restore, or clean up backups:

```bash
veil repair --backup-dir /var/lib/veil/backups --audit-log /var/log/veil/audit.jsonl --yes
veil rollback list --backup-dir /var/lib/veil/backups
veil rollback restore <backup-id> --backup-dir /var/lib/veil/backups --yes
veil rollback cleanup <backup-id> --backup-dir /var/lib/veil/backups --yes
```

Audit logs are JSONL — one line per operation — and are never written during `--dry-run`. The backup directory must be writable.

## Security

- **Panel hidden** — random path, not discoverable by port scanners
- **API token** — required for all management operations
- **Encryption** — secrets encrypted with AES-256-GCM (`/etc/veil/state.key`)
- **TLS 1.2+** — HSTS with 2-year max-age, modern cipher suites only
- **Rate limiting** — protects expensive endpoints (logs, ping, DNS, speedtest)
- **Input validation** — all API inputs validated, no shell injection vectors

## Docker

```bash
docker run -d --name veil --network host \
  -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
  veil-panel/veil:latest serve
```
