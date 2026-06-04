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

### Installation Parameters

You can customize the installer's behavior using options passed to the bash command:

```bash
curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | sudo bash -s -- [options]
```

| Option | Default | Description |
|---|---|---|
| `--panel-access MODE` | `local` | Panel access mode: `local` (loopback only), `direct` (public interface), or `caddy` (automatic HTTPS on a domain). |
| `--domain DOMAIN` | *(empty)* | Domain name to use (required if `--panel-access` is `caddy`). |
| `--email EMAIL` | *(empty)* | ACME email for Let's Encrypt certificates (required for `caddy`). |
| `--panel-port PORT` | `2096` | Port the Panel will listen on. Use `0` to select a random high port. |
| `--profile NAME` | `ru-recommended` | Initial routing rules profile preset. Choices: `default` or `ru-recommended`. |
| `--version TAG` | `latest` | Specify a targeted release tag to install (e.g. `v0.9.9`). |
| `--force` | *(false)* | Force binary download and re-installation even if Veil is already present. |
| `--yes` | *(false)* | Run the installation non-interactively (answers defaults). |
| `--dry-run` | *(false)* | Download the binary but do not perform changes. |

---

## 2. Choosing a Panel Access Mode

Veil provides three exposure profiles for the management Panel:

Before any public exposure, create Panel credentials explicitly:

```bash
sudo veil admin reset
# or
sudo veil admin set --username admin --password 'use-a-long-random-password' --role admin
```

Direct public `veil serve` listeners require both a configured Panel user and an API token. Local loopback listeners can be reached through SSH and are the default.

### Local Mode (Highly Recommended)
Exposes the Panel on the loopback interface (`127.0.0.1:2096`) with a self-signed TLS certificate.
- **Accessing it:** Run an SSH tunnel from your client machine:
  ```bash
  ssh -L 2096:127.0.0.1:2096 user@your-server-ip
  ```
- **URL:** Open `https://127.0.0.1:2096/` in your browser.
- **Why:** Keeps the administration page completely hidden from public port scans and probes.

### Caddy Mode
Exposes the Panel publicly over automatic Let's Encrypt HTTPS on a custom domain name.
- **Accessing it:** Caddy terminates TLS and proxies the traffic under a randomly-generated secret Web base path prefix.
- **URL:** `https://your-domain.com/a1b2c3d4e5f6/` (the path is outputted at the end of the installation).
- **Why:** Safely exposes the panel publicly without exposing raw ports, using standard reverse proxy hardening.
- **Auth:** Use a Panel user/session for browser access. Keep `VEIL_API_TOKEN` set for API clients and automation.

### Direct Mode
Exposes the Panel directly on all interfaces on the configured port using self-signed TLS.
- **URL:** `https://your-server-ip:2096/`
- **Why:** Convenient for quick staging or internal networks.
- **Warning:** Direct exposure of self-signed HTTP endpoints is prone to port scanning and certificate bypass dialog warnings. `veil serve` refuses non-loopback direct exposure unless both `VEIL_API_TOKEN` and a Panel user are configured.

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
| `/etc/veil/` | `0750` | Configuration root, owned by root/veil. Contains secrets and state. |
| `/etc/veil/veil.env` | `0600` | Environment variables, Panel listen settings, and authentication tokens. |
| `/etc/veil/state.json` | `0600` | Persisted Management state containing settings and configured Inbounds. |
| `/etc/veil/state.key` | `0600` | AES-256-GCM encryption key used to encrypt passwords and secrets at rest. |
| `/var/lib/veil/backups/` | `0700` | Compressed configurations created before applying/repairing settings. |
| `/var/log/veil/audit.jsonl` | `0600` | Append-only audit trail logging all install, repair, and rollback events. |
| `/etc/systemd/system/veil.service` | `0644` | Systemd service definition running the core Panel daemon. |
