# Veil

Veil is a management panel for NaiveProxy, Hysteria2, olcRTC, and Mieru. It installs the Panel first; proxy Inbounds are added later from the Panel.

Upgrading to v0.7.3? Read the [upgrade guide](docs/install.md#5-upgrading-from-v072)
first: the state migration requires a pre-upgrade backup for downgrades.

![Veil panel](docs/assets/veil-panel.gif)

## Quick start

One command, answer a few questions, done:

```bash
curl -fsSL https://github.com/mikkelchokolate/Veil/releases/latest/download/install.sh | sh
```

To install the current `main` commit instead of a tagged release (builds from source; not cosign-signed):

```bash
curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install-main.sh | sh
```

`install.sh` runs without root. It downloads and verifies the pinned verifier,
`install-privileged.sh`, the Veil archive, signed checksums, and signed
provenance. Only after every privileged payload passes verification does it
invoke `sudo` itself.

The installer asks for the Panel exposure mode (`local`, `direct`, or `caddy`)
and whether to choose a random high Panel port or use a port you enter. Veil
still installs as a **panel-only** control plane first; you do not need a domain
or Caddy unless you choose Caddy Panel access or add a NaiveProxy Inbound later.

For local access, the installer prints the Panel's self-signed HTTPS address:

```text
Panel access: https://127.0.0.1:<panel-port>/<web-base-path>/
```

or, if you choose Caddy Panel access:

```text
Panel URL: https://vpn.example.com/a1b2c3d4e5f6/
```

The HTTPS Panel URL path is randomly generated — only you know it. Local access
uses a self-signed certificate. Direct mode attempts a trusted IP certificate
and falls back to a self-signed certificate with a warning if issuance fails.

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
sudo veil admin set --username admin --role admin   # prompts for the password without echo
# non-interactive: read it from stdin or a file instead of argv
printf '%s\n' "$PANEL_PASSWORD" | sudo veil admin set --username admin --role admin --password-stdin
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

## Commands

Run `sudo veil` in a terminal to open the management and recovery menu, or
`sudo veil menu` to open it explicitly. It manages local services, logs,
credentials, backups, updates, installation, and selected protocol runtimes even when the
web Panel is unavailable. Changes require confirmation in the menu.

`veil help` prints the full catalog. `veil help <command>` shows flags for one command.

| Command | Purpose |
|---|---|
| `veil help` | Show every command, including nested subcommands. |
| `veil menu` | Open the terminal management and recovery menu; works without the web Panel. |
| `veil install` | Install Panel access and credentials; protocol runtimes are acquired on first use (Caddy is acquired immediately when needed to expose the Panel). |
| `veil serve` | Run the HTTP API and Web Panel. |
| `veil status` | Show managed service status (`--json` for machine output). |
| `veil doctor` | Run host readiness checks (required/optional commands, protocol DNS reachability). |
| `veil admin reset` | Reset the administrator with new random credentials. |
| `veil admin set` | Set or update a user (`--username`, `--role admin\|viewer`; password via interactive prompt, `--password-stdin` or `--password-file`; legacy `--password` is visible in the process list). An existing user keeps their TOTP factor, recovery codes and passkeys; add `--clear-2fa` to remove them when the authenticator is lost. |
| `veil admin show` | List registered users and roles. |
| `veil admin rotate-key` | Rotate the AES state key and re-encrypt state. |
| `veil config validate` | Validate a Management state file without starting a server. |
| `veil runtime install` | Preinstall or repair protocol runtime binaries (`--only`, `--bin-dir`); normally acquired on first use. |
| `veil backup create` | Create an encrypted archive of Panel state and the encryption key. |
| `veil backup list` | List managed backup archives. |
| `veil backup verify` | Decrypt and verify a backup without writing state. |
| `veil backup restore` | Restore state and key from a backup. |
| `veil backup prune` | Apply daily/weekly/monthly retention (`--dry-run` to preview). |
| `veil backup schedule enable` | Store the passphrase and enable the daily backup timer. |
| `veil backup schedule disable` | Disable the daily backup timer. |
| `veil repair` | Repair managed generated files without arbitrary side effects. |
| `veil rollback list` | List configuration-file backups. |
| `veil rollback restore` | Restore files from a configuration backup. |
| `veil rollback cleanup` | Remove a configuration backup after a successful restore. |
| `veil update` | Download and install the latest Veil release (`--yes --staged`). |
| `veil uninstall` | Remove the Panel, services, configuration, and state. |
| `veil version` | Print the Veil version (`--check` to compare with the latest release). |

Everyday examples follow. For **v0.7.2 → v0.7.3**, use the
[dedicated upgrade sequence](docs/install.md#5-upgrading-from-v072) instead
of the generic staged-update example: binary rollback cannot undo its migration.

```bash
veil status
veil version --check
veil update --yes --staged
veil uninstall --yes              # also removes config/state; reinstall starts fresh
veil uninstall --yes --keep-data  # preserve credentials and config across a reinstall
```

**Privilege separation:** the Panel runs as the locked `veil` user with no
capabilities. Root-only host mutations go through the allowlisted
`veil-helper.socket`.

The Panel records authentication, user, configuration, apply, and service
actions in a structured, redacted, rotated JSONL audit log. With the default
state path it is stored at `/var/lib/veil/audit/panel.jsonl`. Encrypted backup
directories must be writable by the process performing each operation.

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

## More documentation

- [Installation](docs/install.md) — access modes, flags, and compiling from source
- [Troubleshooting](docs/troubleshooting.md) — diagnostics, logs, and rollback
- [Panel operations](docs/operations.md) — live validation and apply previews
- [Hardening](docs/HARDENING.md) — deployment hardening and signed releases
- [Disaster recovery](docs/disaster-recovery.md) — encrypted backups and restore
- [Known limitations](docs/known-limitations.md)
- [Changelog](CHANGELOG.md)
- [Security policy](SECURITY.md)
