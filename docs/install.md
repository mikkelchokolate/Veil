# Veil Installation Guide

This guide describes how to install and configure Veil on your server, including running it from source or via the automated installer.

## Prerequisites
- **Operating System:** Linux (systemd is required for managing protocol runtimes on bare-metal).
- **Permissions:** Root access (`sudo`) is required to write systemd unit files, configure local firewalls, and manage system services.

---

## 1. Automated Installation (Quick Start)

The easiest way to install Veil is using the official quick-start script. This script automatically detects your OS architecture (amd64/arm64), downloads the latest binary, verifies its checksum, and sets up the panel.

```bash
curl -fsSL https://github.com/mikkelchokolate/Veil/releases/latest/download/install.sh | sh
```

To install the current `main` commit instead of a tagged release, use the
source installer. It fetches origin/`main`, builds the Panel, then uses the
same privileged installer handoff. This path is not cosign-signed.

```bash
curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install-main.sh | sh
```

After install, `veil help` lists every command. `veil help <command>` shows
flags for one command.

The piped `install.sh` is an unprivileged bootstrap. It verifies the pinned
verifier, `install-privileged.sh`, the Veil archive, signed checksums, and signed
provenance before invoking `sudo` itself.

Interactive installs ask for the Panel exposure mode (`local`, `direct`, or
`caddy`) and whether Veil should choose a random high Panel port or use a port
you enter manually. Choose `local` + random port for the safest default.

### Installation Parameters

You can customize the installer's behavior using options passed to the bash command:

```bash
curl -fsSL https://github.com/mikkelchokolate/Veil/releases/latest/download/install.sh | sh -s -- [options]
```

| Option | Default | Description |
|---|---|---|
| `--panel-access MODE` | prompted interactively; `local` with `--yes` | Panel access mode: `local` (loopback only), `direct` (public interface), or `caddy` (automatic HTTPS on a domain). |
| `--domain DOMAIN` | *(empty)* | Domain name to use (required if `--panel-access` is `caddy`). |
| `--email EMAIL` | *(empty)* | ACME email for Let's Encrypt certificates (used for `caddy`; optional for `direct` IP certificates). |
| `--panel-port PORT` | prompted interactively; `2096` with `--yes` | Port the Panel will listen on. Use `0` to select a random high port. |
| `--le-ip-cert` | `true` for `direct`, otherwise ignored | Obtain a trusted Let's Encrypt IP certificate in `direct` mode (short-lived; see Direct Mode). |
| `--le-ip-cert-port PORT` | `80` | Port used for the Let's Encrypt HTTP-01 challenge listener. |
| `--profile NAME` | `ru-recommended` | Initial routing rules profile preset. The implemented value is `ru-recommended`. |
| `--version TAG` | `latest` | Specify a targeted release tag to install (e.g. `v0.9.9`). |
| `--force` | *(false)* | Force binary download and re-installation even if Veil is already present. |
| `--yes` | *(false)* | Run the installation non-interactively (answers defaults). |
| `--dry-run` | *(false)* | Download the binary but do not perform changes. |

The `install-main.sh` bootstrap accepts the same `--panel-access`, `--domain`,
`--email`, `--yes`, `--force`, and related flags and forwards them to
`install-privileged.sh` after the `main` binary has been built.

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
- **URL:** Open the address printed at the end of install (`https://127.0.0.1:<panel-port>/<web-base-path>/`). The secret Web base path is required; `/` 404s.
- **Why:** Keeps the administration page completely hidden from public port scans and probes.

### Caddy Mode
Exposes the Panel publicly over automatic Let's Encrypt HTTPS on a custom domain name.
- **Accessing it:** Caddy terminates TLS and proxies the traffic under a randomly-generated secret Web base path prefix.
- **URL:** `https://your-domain.com/a1b2c3d4e5f6/` (the path is outputted at the end of the installation).
- **Why:** Safely exposes the panel publicly without exposing raw ports, using standard reverse proxy hardening.
- **Auth:** Use a Panel user/session for browser access. Keep `VEIL_API_TOKEN` set for API clients and automation.

### Direct Mode
Exposes the Panel directly on all interfaces on the configured port.

By default, `direct` mode obtains a trusted Let's Encrypt certificate for the
server's public IP address. Veil requests the `shortlived` certificate profile
(3-day validity) through `acme.sh` in standalone mode and registers an `acme.sh`
`reloadcmd` that restarts the Panel on renewal. This removes the browser
certificate warning and prevents cached HSTS policies from causing redirect
loops when accessing the panel by IP. The certificate's SAN is the public IP
address (no `CN`).

- **URL:** `https://your-server-ip:<panel-port>/<web-base-path>/` (the path is printed at the end of install). `/` without that prefix 404s.
- **Requirements:** Port `80/tcp` must be free and reachable from the internet
  during install/repair so the Let's Encrypt HTTP-01 challenge can complete.
- **Fallback:** If the IP certificate cannot be issued (for example, port 80 is
  already in use), Veil falls back to a self-signed certificate and prints a
  warning.
- **Why:** Convenient for quick staging or internal networks.
- **Warning:** Direct exposure is prone to port scanning. `veil serve` refuses
  non-loopback direct exposure unless TLS, `VEIL_API_TOKEN`, a Panel user, and
  authenticated metrics are configured. Plain public HTTP is rejected; the
  emergency `--unsafe-allow-public-http` /
  `VEIL_UNSAFE_ALLOW_PUBLIC_HTTP=true` override must never be used on an
  untrusted network.

---

## 3. Hysteria2 Inbound Certificates

Hysteria2 requires TLS for its QUIC handshake (the client verifies a
certificate for its SNI). When you give a Hysteria2 Inbound a domain, Veil
obtains a real ACME certificate for it so clients connect without `insecure`:

- **Reusing the Panel or a NaiveProxy Inbound's domain** — the Inbound reuses
  the certificate Caddy already manages for that domain via `tls-alpn-01`. No
  additional port or challenge listener is required.
- **A Hysteria2-only domain** (any other domain resolving to the host) — Veil
  switches that domain to the HTTP-01 challenge and adds a dedicated ACME
  challenge listener on TCP `:80`, then syncs the issued certificate to the
  Inbound. This needs TCP `:80` free (or already served by Caddy) and the
  domain's DNS pointing at the host. If `:80` is unavailable the apply still
  succeeds with a warning and the Inbound keeps running on a self-signed
  certificate until `:80` is freed and re-applied.
- **No domain** — the Inbound uses a self-signed certificate and clients must
  use `insecure` (unchanged from before).

Add the domain and email on the Hysteria2 Inbound in the Panel; no extra Caddy
configuration is needed.

---

## 4. Protocol Runtime Binaries

Veil's managed systemd units invoke external runtime binaries:

| Protocol | Binary | Unit | Source |
|---|---|---|---|
| NaiveProxy | `caddy` | `veil-caddy@.service` | `caddyserver/caddy` GitHub release |
| Hysteria2 | `hysteria` | `veil-hysteria2@.service` | `apernet/hysteria` GitHub release |
| Mieru | `mita` | `veil-mieru.service` | `enfein/mieru` GitHub release |
| WARP | `sing-box` | `veil-warp.service` | `SagerNet/sing-box` GitHub release |
| olcRTC | `olcrtc` | `veil-olcrtc@.service` | built from source with `go install` |

`veil install` provisions these automatically after the Panel is configured.
Release binaries are downloaded for your architecture and verified against the
upstream checksums (SHA-256 or SHA-512) where the project publishes them. olcRTC
ships no release binaries, so it is built from source and requires a Go
toolchain on the host; if `go` is not present that runtime is skipped with a
warning and the rest still install.

Install or repair the runtimes at any time:

```bash
sudo veil runtime install                  # all runtimes
sudo veil runtime install --only mieru,hysteria2
```

Without these binaries, protocol services fail to start with systemd status
`203/EXEC` ("Failed to locate executable"). Run `veil doctor` to confirm each
runtime is present.

---

## 5. Manual Building from Source

If you prefer to compile Veil from source, follow these instructions.

### Go Module Configuration
The project uses the Go module path `github.com/mikkelchokolate/Veil`, which is canonical and matches the GitHub repository URL. You can build the binary from source or install it directly using Go toolchain commands.

### Build Steps

1. **Install Go:** Ensure you have Go 1.27 or newer installed (see `go.mod`):
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

## 6. File and Directory Structure

When installed natively on a Linux host, Veil manages the following directories and configurations:

| Path | Mode | Description |
|---|---|---|
| `/usr/local/bin/veil` | `0755` | The compiled Veil management daemon binary. |
| `/etc/veil/` | `0750` | Configuration root, owned by root/veil. Contains environment files, keys, and generated runtime material. |
| `/etc/veil/veil.env` | `0640 root:veil` | Environment variables, Panel listen settings, and authentication tokens readable by the Panel service. |
| `/var/lib/veil/state.json` | `0600 veil:veil` | Persisted Management state containing settings and configured Inbounds. |
| `/etc/veil/state.key` | `0640 root:veil` | AES-256-GCM encryption key readable by the Panel but writable only through the privileged helper. |
| `/etc/veil/backup.passphrase` | `0600 root:root` | Install-generated passphrase for Panel backup create/restore and the optional daily backup timer. |
| `/var/lib/veil/backups/` | `0700 root:root` | Verified encrypted disaster-recovery archives managed through the privileged helper and backup timer. |
| `/var/lib/veil/staging/` | `0700 veil:veil` | Candidate generated configuration awaiting privileged promotion. |
| `/var/lib/veil/updates/` | `0700 veil:veil` | Downloaded release archive and checksum material awaiting helper verification. |
| `/var/lib/veil/migration-backups/` | `0700 root:root` | Root-owned safety copies created before ownership or permission migration. |
| `/var/lib/veil/sessions.json` | `0600 veil:veil` | Hashed browser session and CSRF state; raw bearer values are never persisted. |
| `/var/lib/veil/audit/panel.jsonl` | `0600 veil:veil` | Rotated, redacted Panel authentication and mutation audit history. |
| `/var/log/veil/audit.jsonl` | `0600` | Append-only audit trail logging all install, repair, and rollback events. |
| `/etc/systemd/system/veil.service` | `0644` | Hardened non-root Panel service running as the `veil` account. |
| `/etc/systemd/system/veil-helper.service` | `0644` | Root privileged helper with an allowlisted operation protocol. |
| `/etc/systemd/system/veil-helper.socket` | `0644` | Socket activation for `/run/veil/helper.sock` with `root:veil 0660` access. |
| `/etc/systemd/system/veil-backup.service` | `0644` | Hardened oneshot encrypted backup and retention job. |
| `/etc/systemd/system/veil-backup.timer` | `0644` | Daily scheduler for `veil-backup.service`. |

Passing an alternative `--systemd-dir` is a staging/package-build mode. Veil
writes the requested files but does not create the `veil` service account,
change current-host ownership, or invoke `systemctl`. Native host installation
keeps the default `/etc/systemd/system` path and always applies those hardening
steps.

Enable scheduled encrypted backups after installation:

```bash
sudo veil backup schedule enable \
  --passphrase-file /root/veil-backup-passphrase
systemctl list-timers veil-backup.timer
```

See [Disaster Recovery And Key Lifecycle](disaster-recovery.md) before relying
on local backups or performing a restore.

By default, removal stops and removes managed units, the binary, **and** the
configuration and state in `/etc/veil` and `/var/lib/veil` (including encrypted
backups), so a later install starts fresh with a new password and panel path.
The locked `veil` account is preserved. Review and export backups first:

```bash
sudo veil uninstall --yes
```

To preserve configuration and credentials across a reinstall — for example
before an in-place upgrade — keep the data directories instead:

```bash
sudo veil uninstall --yes --keep-data
```

`--purge` remains accepted as an explicit alias for the default full removal and
always overrides `--keep-data`:

```bash
sudo veil uninstall --yes --purge
```
