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

After install, `sudo veil` opens the terminal management and recovery menu.
`veil help` lists every command; `veil help <command>` shows its flags.

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
sudo veil admin set --username admin --role admin   # prompts for the password without echo
# non-interactive: read it from stdin or a file instead of argv
printf '%s\n' "$PANEL_PASSWORD" | sudo veil admin set --username admin --role admin --password-stdin
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
through `acme.sh` and runs its own renewal worker; a successful live apply
with service actions or `veil repair` can also renew it when due. The renewal
reload command restarts the Panel. This removes the browser
certificate warning and prevents cached HSTS policies from causing redirect
loops when accessing the panel by IP. The certificate's SAN is the public IP
address (no `CN`).

- **URL:** `https://your-server-ip:<panel-port>/<web-base-path>/` (the path is printed at the end of install). `/` without that prefix 404s.
- If an older installer printed a URL without the panel port, add `:2096`
  (or the port selected during install) after the IP address, keeping the path.
  If public IP detection is unavailable, pass `--public-ip YOUR_SERVER_IP`
  to `veil install` so it can print the correct address.
- **Requirements:** Port `80/tcp` must be reachable from the internet during
  issuance and renewal. The HTTP-01 listener needs the port free, or a
  Veil-managed Caddy route that forwards the challenge to the internal issuer.
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
| NaiveProxy | `caddy` | `veil-caddy.service` | pinned Caddy + Naive forwardproxy source build |
| Hysteria2 | `hysteria` | `veil-hysteria2@.service` | `apernet/hysteria` GitHub release |
| Mieru | `mita` | `veil-mieru.service` | `enfein/mieru` GitHub release |
| WARP | `sing-box` | `veil-warp.service` | `SagerNet/sing-box` GitHub release |
| olcRTC | `olcrtc` | `veil-olcrtc@.service` | built from source with `go install` |

`veil install` installs the Panel without downloading protocol runtimes.
The exception is `--panel-access caddy`: the installer ensures a verified,
pinned Caddy so the Panel can be reached through its domain.

When you first apply an enabled Inbound, Veil acquires only its missing
runtime. Enabling WARP acquires `sing-box`. Existing runtimes are reused,
and creating a disabled Inbound does not trigger a download. The same applies
when applying configuration through the CLI.

Release binaries are downloaded for your architecture and verified before
installation. Caddy and olcRTC are built from pinned sources; Veil acquires
the required Go toolchain when needed. The first use may take longer while
the required runtime is downloaded or built.

Preinstall selected runtimes before first use (for example, before taking the
server offline), or repair them at any time:

```bash
sudo veil runtime install                  # explicitly install all runtimes
sudo veil runtime install --only mieru,hysteria2
```

Without these binaries, protocol services fail to start with systemd status
`203/EXEC` ("Failed to locate executable"). Run `veil doctor` to confirm each
runtime is present.

---

## 5. Upgrading from v0.7.2

Schedule a maintenance window. v0.7.3 writes **state schema 6** and migrates
the database; an older binary cannot read the migrated state. A binary-only
rollback, including `veil update --staged`, does not undo those migrations.

### Back up before the first new start

Using the **old version**, create and verify an encrypted backup. Supply an
existing protected passphrase file (root-readable, mode `0600`); do not put
the passphrase in a command-line argument. Replace the verification example's
archive path with the actual path printed by `backup create`.

```bash
sudo veil backup create --passphrase-file /root/veil-backup-passphrase \
  --output-dir /var/lib/veil/backups
sudo veil backup verify /var/lib/veil/backups/veil_backup_YYYYMMDD_HHMMSS.tar.gz.enc \
  --passphrase-file /root/veil-backup-passphrase
```

The v0.7.2 backup includes a consistent `state.json`, `veil.db` and `state.key`.
Copy the verified archive and its passphrase safely off the server, keeping
the passphrase separate. Retain the old release/package for disaster recovery.
For custom layouts, pass the actual `--state /CUSTOM/var/state.json` and
`--key-path /CUSTOM/etc/state.key` to backup creation; the database is resolved
beside the state file. Stop if backup creation or verification fails.

### Binary-managed installations

`veil update` targets the current stable GitHub Latest release. Check its
preview and version before proceeding; these instructions apply when that
release is v0.7.3. After verifying the backup, stop the old processes before
replacing the binary: otherwise an automatic restart could launch the new
Panel while the old helper is still running. Run each step separately and
stop if any command fails. Do not add `--restart` or `--staged` to the updater.
Ignore the updater's standalone restart hint; continue with repair below.

```bash
sudo veil update --dry-run
sudo systemctl stop veil.service veil-helper.service veil-helper.socket
sudo veil update --yes
sudo veil repair --dry-run
sudo veil repair --yes
sudo systemctl start veil-helper.socket veil-helper.service veil.service
veil version
sudo veil doctor
sudo veil status
```

Review the repair preview before applying it. Repeat your installation's
`--etc-dir`, `--var-dir` and `--systemd-dir` on both repair commands when they
differ from the defaults. Repair may restart changed services itself; always
run the explicit start immediately afterward, helper socket first. A no-op
repair need not start stopped services. Repairs refresh install-managed units,
including Mieru's DNS fallback hook; a binary update alone does not.

If the updater fails and `veil version` still reports the old version, run
the explicit `systemctl start` line to restore the old services. If the binary
has already changed, or repair fails, keep services stopped while inspecting
the error and follow the [disaster-recovery guide](disaster-recovery.md) with
the matching pre-upgrade backup and binary. Do not assume a binary rollback
reverses a migration.

### Source-built main installations

Updaters with source-history verification (added after v0.7.3) allow a normal
Panel/CLI update from `main-<short SHA> (<full SHA>)` only when GitHub confirms
that the exact running commit is included in the target release. The signed
download must match that target commit. Newer/diverged source builds, unknown
stamps and unavailable proof still require an explicit operator override.

An older installed updater cannot acquire this behavior before replacing
itself. For a one-time transition, first verify the backup and independently
confirm that the target release contains the running full commit. Follow the
binary-managed sequence above: use `sudo veil update --dry-run --force` for
the preview and `sudo veil update --yes --force` for installation. Keep the
service/socket stop step between them.

Continue with repair and the explicit helper-first start from that sequence;
do not add `--restart` or `--staged`. Force overrides version ordering, not
signature/provenance verification, and does not make an incompatible schema
downgrade safe. If the release does not include the running commit, keep the
source build or use a matching pre-migration backup and binary for recovery.

### Native packages and containers

For `.deb`, `.rpm` or `.apk` installations, install the matching v0.7.3 package
with the package manager instead of overwriting its binary with `veil update`.
Package hooks reload systemd and restart the units the package owns; they do
not re-render `veil` drop-ins under `/etc/systemd/system`, so the pre-upgrade
backup and post-upgrade checks are still required. Run `veil repair` after an
upgrade if you installed with custom paths. For containers, back up the
mounted state and key before replacing the image with
`ghcr.io/mikkelchokolate/veil:0.7.3`. Rootless containers still cannot perform
the host helper's systemd/firewall operations.

### Custom install paths

`veil repair` works on the tree the host is installed with: it discovers the
layout from the installed `veil.service` drop-in and `veil.env`, and refuses to
run when that layout differs from the packaged `/etc/veil` + `/var/lib/veil`
defaults. Pass the paths explicitly to fix a custom install:

```bash
sudo veil repair --etc-dir /opt/veil/etc --var-dir /opt/veil/var --systemd-dir /etc/systemd/system
```

This re-renders the install drop-ins (including the key path they read: since
v0.7.3 the helper, backup and Panel units take `VEIL_KEY_PATH` from `veil.env`
instead of a hardcoded literal or `--key-path`). Upgraded custom installs keep
their pre-0.7.3 drop-ins until that command runs, and `veil admin rotate-key`
refuses to rotate while such a drop-in is still present.

An install that writes its units outside `/etc/systemd/system` records the
directory as `VEIL_SYSTEMD_DIR` in `veil.env`. `veil repair`, the `rotate-key`
preflight and `veil doctor` read it, so they keep working on the real unit tree;
pass `--systemd-dir` to override it for one run. A directory the live systemd
does not load is treated as staging: `veil repair` writes the unit files there
and reports that it skipped the systemd actions. Passing an empty
`--systemd-dir ""` is refused.

### Install state paths and reinstalls

`veil install` takes the state key and state file from the installed `veil.env`
(`VEIL_KEY_PATH`, `VEIL_STATE_PATH`) when they point inside the tree it writes,
so the documented path-moving `veil admin rotate-key --new-key-path` survives a
re-install: the rendered `veil.env` keeps the rotated paths and the existing
panel state is read with the matching key. Without that, a re-install failed on
the pre-rotation key or, when the state had moved, silently started a fresh
panel next to the real one.

When the recorded path lies outside `--etc-dir`/`--var-dir` — or an existing
leaf there is a symlink that resolves outside the tree — the command stops
before creating anything (no key, no state file, no `veil.env` rewrite) and
prints the recorded path with two ways forward:

- re-run with the `--etc-dir`/`--var-dir` that hold the real tree, or
- pass `--reset-install-state` to install a fresh panel against the packaged
  layout on purpose. The flag only ignores the recorded paths: the key and state
  files on disk are left untouched, so take a backup first if you still need
  them.

The refusal also covers `--dry-run` and `--check-only`: the recorded paths are
resolved before the preview, so a foreign tree is reported instead of planned.

```bash
# keep the installed (= rotated) state paths
sudo veil install --etc-dir /opt/veil/etc --var-dir /opt/veil/var

# deliberately start fresh on the packaged layout, leaving the old files on disk
sudo veil install --reset-install-state
```

### Check the upgraded installation

Sign in and run one confirmed live Apply to refresh generated runtime
configuration and certificate routes, then check service status and client
connectivity. This can restart protocol services and interrupt connections.
First-use runtime installation may need internet access, including when an
existing binary is untracked or no longer matches the pinned version.

Pre-upgrade unsealed revisions cannot be selected for apply rollback. If the
applied revision lagged the desired revision, subscription feeds wait for a
successful apply. Inspect startup warnings for legacy tokens that need
re-issuance. Do not edit the schema number or delete `veil.db`/`state.key` to
bypass a migration failure; recover with the matching pre-upgrade backup and
compatible binary. Use `sudo veil menu` for service logs, diagnostics and
recovery if the web Panel is unavailable.

---

## 6. Manual Building from Source

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

## 7. File and Directory Structure

When installed natively on a Linux host, Veil manages the following directories and configurations:

| Path | Mode | Description |
|---|---|---|
| `/usr/local/bin/veil` | `0755` | The compiled Veil management daemon binary. |
| `/etc/veil/` | `0751` | Configuration root, owned by root/veil. Contains environment files, keys, and generated runtime material. The world execute bit lets `veil-proxy` protocol units traverse into the `root:veil-proxy` subdirectories without listing the root. |
| `/etc/veil/veil.env` | `0640 root:veil` | Environment variables, Panel listen settings, and authentication tokens readable by the Panel service. |
| `/var/lib/veil/state.json` | `0600 veil:veil` | Persisted Management state containing settings and configured Inbounds. |
| `/etc/veil/state.key` | `0640 root:veil` | AES-256-GCM encryption key readable by the Panel but writable only through the privileged helper. |
| `/etc/veil/backup.passphrase` | `0600 root:root` | Install-generated passphrase for Panel backup create/restore and the optional daily backup timer. |
| `/var/lib/veil/backups/` | `0700 root:root` | Verified encrypted disaster-recovery archives managed through the privileged helper and backup timer. |
| `/var/lib/veil/staging/` | `0700 veil:veil` | Candidate generated configuration awaiting privileged promotion. |
| `/var/lib/veil/updates/` | `0700 veil:veil` | Downloaded release archive and checksum material awaiting helper verification. |
| `/var/lib/veil/migration-backups/` | `0700 root:root` | Root-owned safety copies created before ownership or permission migration. |
| `/var/lib/veil/sessions.json`, `sessions.json.journal`, `sessions.json.lock` | `0600 veil:veil` | Hashed browser session and CSRF state; raw bearer values are never persisted. The journal records session changes and revocation intents; the lock file serializes store rewrites between the Panel and the root CLI. |
| `/etc/veil/generated/`, `/etc/veil/tls/`, `/etc/veil/panel/`, `/etc/veil/certs/`, `/etc/veil/www/` | `0750 root:veil-proxy` dirs, `0640 root:veil-proxy` files | Runtime-readable material shared with the `veil-proxy` services (Caddy, protocol units): rendered configuration, Panel TLS, ACME output, and the naive fallback site. |
| `/run/veil/` | `0711 root:root` | Helper socket parent: traverse-only so no `veil`-uid process can replace `/run/veil/helper.sock` (`root:veil 0660`). |
| `/run/veil-mieru/` | `0750 veil-mita:veil-mita` | mita runtime directory holding the appctl socket `mita.sock` (`0770 veil-mita:veil-mita` via `UMask=0007`): only the daemon and the `veil` panel account (a `veil-mita` group member) can connect — `veil-proxy` edge units cannot traverse it. |
| `/var/lib/mita/` | `0700 veil-mita:veil-mita` | mieru daemon state directory owned by the dedicated `veil-mita` identity (`veil-mieru.service` runs `User=veil-mita`, not the shared `veil-proxy` account). |
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

By default, removal stops and removes managed units — both the units `veil
install` writes under `/etc/systemd/system` and any packaged units left under
`/lib`/`/usr/lib/systemd/system` — the binary, the QUIC sysctl drop-in
(`/etc/sysctl.d/99-veil-quic.conf`), **and** the configuration and state in
`/etc/veil` and `/var/lib/veil` (including encrypted backups), so a later
install starts fresh with a new password and panel path.
A full removal also deletes the UFW allow rules Veil created (every rule
commented `Veil …`: panel, proxy ports and the ACME `80/tcp` challenge), so no
port stays open for whatever binds it next. The `Veil management SSH` allow is
kept so an active UFW cannot lock you out; delete it by hand if you no longer
need it. `--keep-data` leaves the firewall untouched, and the plan printed
before removal lists the firewall step.
A full removal also deletes the runtime binaries `veil runtime install` put in
the install dir (`caddy`, `hysteria`, `sing-box`, `mita`, `olcrtc`) together
with its provenance record (`.veil-runtime-generation.json`). Only binaries
whose bytes still match the SHA-256 recorded at install time are removed; a
system or hand-built `caddy` at the same path is never touched, and without
the record nothing but the `veil` binary goes. `--keep-data` keeps the
runtimes, because `veil install` does not fetch them and an in-place upgrade
needs them in place.
The Caddy/mita runtime state dirs (`/var/lib/caddy`, `/var/lib/mita`) share
their names with the stock system Caddy/mita packages, so they are removed only
when a Veil service account (`veil-proxy`, `veil-mita`, `veil`) owns them or
they carry a `.veil-managed` marker; otherwise the uninstall refuses before
deleting anything (use `--keep-data`, or point `--caddy-state-dir` /
`--mita-state-dir` elsewhere). A `veil-*` prefix on an operator directory does
not count as proof either.
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
