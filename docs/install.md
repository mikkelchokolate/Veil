# Veil Installation Guide

This guide describes how to install and configure Veil on your server, including running it from source or via the automated installer.

## Prerequisites
- **Operating System:** Linux (systemd is required for managing protocol runtimes on bare-metal).
- **Permissions:** Root access (`sudo`) is required to write systemd unit files, configure local firewalls, and manage system services.

---

## 1. Automated Installation (Quick Start)

The easiest way to install Veil is using the official quick-start script. This script automatically detects your OS architecture (amd64/arm64), downloads the latest binary, verifies its checksum, and sets up the panel.

```bash
curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | sudo bash
```

Interactive installs ask for the Panel exposure mode (`local`, `direct`, or
`caddy`) and whether Veil should choose a random high Panel port or use a port
you enter manually. Choose `local` + random port for the safest default.

### Installation Parameters

You can customize the installer's behavior using options passed to the bash command:

```bash
curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | sudo bash -s -- [options]
```

| Option | Default | Description |
|---|---|---|
| `--panel-access MODE` | prompted interactively; `local` with `--yes` | Panel access mode: `local` (loopback only), `direct` (public interface), or `caddy` (automatic HTTPS on a domain). |
| `--domain DOMAIN` | *(empty)* | Domain name to use (required if `--panel-access` is `caddy`). |
| `--email EMAIL` | *(empty)* | ACME email for Let's Encrypt certificates (required for `caddy`). |
| `--panel-port PORT` | prompted interactively; `2096` with `--yes` | Port the Panel will listen on. Use `0` to select a random high port. |
| `--profile NAME` | `ru-recommended` | Initial routing rules profile preset. Choices: `default` or `ru-recommended`. |
| `--version TAG` | `latest` | Specify a targeted release tag to install (e.g. `v0.9.9`). |
| `--force` | *(false)* | Force binary download and re-installation even if Veil is already present. |
| `--yes` | *(false)* | Run the installation non-interactively (answers defaults). |
| `--dry-run` | *(false)* | Download the binary but do not perform changes. |

---

## 2. Choosing a Panel Access Mode

Veil provides three exposure profiles for the management Panel:

An unconfigured `local` installation presents a first-run page only on its
loopback listener. Create the initial administrator there, then sign in.

Before changing an installation to `direct` or `caddy`, create Panel
credentials explicitly if setup has not already been completed:

```bash
sudo veil admin reset
# or
sudo veil admin set --username admin --password 'use-a-long-random-password' --role admin
```

Direct public `veil serve` listeners require native TLS, a configured Panel
user, an API token, and authenticated metrics. Caddy exposure requires a Panel
user and authenticated metrics even though the Veil listener stays on
loopback. Local loopback listeners can be reached through SSH and are the
recommended interactive choice.

### Local Mode (Highly Recommended)
Exposes the Panel on the loopback interface (`127.0.0.1:<panel-port>`) with a self-signed TLS certificate.
- **Accessing it:** Run an SSH tunnel from your client machine:
  ```bash
  ssh -L <panel-port>:127.0.0.1:<panel-port> user@your-server-ip
  ```
- **URL:** Open `https://127.0.0.1:<panel-port>/` in your browser.
- **Why:** Keeps the administration page completely hidden from public port scans and probes.

### Caddy Mode
Exposes the Panel publicly over automatic Let's Encrypt HTTPS on a custom domain name.
- **Accessing it:** Caddy terminates TLS and proxies the traffic under a randomly-generated secret Web base path prefix.
- **URL:** `https://your-domain.com/a1b2c3d4e5f6/` (the path is outputted at the end of the installation).
- **Why:** Safely exposes the panel publicly without exposing raw ports, using standard reverse proxy hardening.
- **Auth:** Use a Panel user/session for browser access. Keep `VEIL_API_TOKEN` set for API clients and automation.

### Direct Mode
Exposes the Panel directly on all interfaces on the configured port using self-signed TLS.
- **URL:** `https://your-server-ip:<panel-port>/`
- **Why:** Convenient for quick staging or internal networks.
- **Warning:** Direct exposure with a self-signed certificate is prone to port
  scanning and certificate bypass dialog warnings. `veil serve` refuses
  non-loopback direct exposure unless TLS, `VEIL_API_TOKEN`, a Panel user, and
  authenticated metrics are configured. Plain public HTTP is rejected; the
  emergency `--unsafe-allow-public-http` /
  `VEIL_UNSAFE_ALLOW_PUBLIC_HTTP=true` override must never be used on an
  untrusted network.

---

## 3. Manual Building from Source

If you prefer to compile Veil from source, follow these instructions.

### Go Module Configuration
The project uses the Go module path `github.com/mikkelchokolate/Veil`, which is canonical and matches the GitHub repository URL. You can build the binary from source or install it directly using Go toolchain commands.

### Build Steps

1. **Install Go:** Ensure you have Go 1.25.0 or newer installed:
   ```bash
   go version
   ```
2. **Clone the repository:**
   ```bash
   git clone https://github.com/mikkelchokolate/Veil.git
   cd Veil
   ```
3. **Compile the binary:**
   Use the Makefile target `make build` (or compile using the `go` command directly):
   ```bash
   # Using Make
   make build

   # Using Go directly
   go build -o bin/veil ./cmd/veil
   ```
   The compiled executable will be placed in the `bin/` directory as `bin/veil`.
4. **Run Unit and E2E Tests:**
   Ensure the build is stable on your architecture:
   ```bash
   make test
   make e2e
   ```

---

## 4. File and Directory Structure

When installed natively on a Linux host, Veil manages the following directories and configurations:

| Path | Mode | Description |
|---|---|---|
| `/usr/local/bin/veil` | `0755` | The compiled Veil management daemon binary. |
| `/etc/veil/` | `0750` | Configuration root, owned by root/veil. Contains environment files, keys, and generated runtime material. |
| `/etc/veil/veil.env` | `0600` | Environment variables, Panel listen settings, and authentication tokens. |
| `/var/lib/veil/state.json` | `0600` | Persisted Management state containing settings and configured Inbounds. |
| `/etc/veil/state.key` | `0600` | AES-256-GCM encryption key used to encrypt passwords and secrets at rest. |
| `/etc/veil/backup.passphrase` | `0600` | Optional root-owned passphrase used by the scheduled backup service and Panel backup controls. |
| `/var/lib/veil/backups/` | `0700` | Verified encrypted disaster-recovery archives managed by the backup timer and Panel. |
| `/var/lib/veil/sessions.json` | `0600` | Hashed browser session and CSRF state; raw bearer values are never persisted. |
| `/var/lib/veil/audit/panel.jsonl` | `0600` | Rotated, redacted Panel authentication and mutation audit history. |
| `/var/log/veil/audit.jsonl` | `0600` | Append-only audit trail logging all install, repair, and rollback events. |
| `/etc/systemd/system/veil.service` | `0644` | Systemd service definition running the core Panel daemon. |
| `/etc/systemd/system/veil-backup.service` | `0644` | Hardened oneshot encrypted backup and retention job. |
| `/etc/systemd/system/veil-backup.timer` | `0644` | Daily scheduler for `veil-backup.service`. |

Enable scheduled encrypted backups after installation:

```bash
sudo veil backup schedule enable \
  --passphrase-file /root/veil-backup-passphrase
systemctl list-timers veil-backup.timer
```

See [Disaster Recovery And Key Lifecycle](disaster-recovery.md) before relying
on local backups or performing a restore.
