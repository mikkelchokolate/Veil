# Veil Hardening Guide

This guide describes how to operate Veil securely. It complements
[`SECURITY.md`](../SECURITY.md) (vulnerability reporting) and focuses on
deployment hardening for the Panel and managed protocol runtimes.

## Threat model in one paragraph

Veil is a privileged control plane: it writes systemd units, renders proxy
configuration, holds protocol credentials, and exposes an HTTP management
Panel. The primary assets to protect are (1) the Panel itself (authenticated
admin access), (2) credentials at rest in Management state, and (3) the
integrity of downloaded release binaries and routing data. The sections below
map to each of these.

## 1. Panel access

Veil offers three Panel access modes, in increasing order of exposure:

| Mode     | Binding            | TLS                | Recommended for |
| -------- | ------------------ | ------------------ | --------------- |
| `local`  | loopback only      | self-signed Panel  | default; reach over SSH tunnel |
| `direct` | public interface   | self-signed Panel  | quick access where Caddy is not wanted |
| `caddy`  | Caddy reverse proxy| ACME (Let's Encrypt) | production, public domains |

Guidance:

- **Prefer `local` + SSH tunnel.** `ssh -L <panel-port>:127.0.0.1:<panel-port> user@host` keeps
  the Panel off the public internet entirely.
- **Use `caddy` for anything public.** It terminates real HTTPS and serves the
  Panel behind a random **Web base path** that acts as an unguessable prefix.
- **Avoid `direct` on untrusted networks** — it exposes a self-signed endpoint
  publicly, which trains operators to click through certificate warnings.

## 2. API authentication

Public listeners are fail-closed. `veil serve --listen 0.0.0.0:...` (or any
other non-loopback address) requires both:

1. API token auth from `--auth-token` or `VEIL_API_TOKEN`.
2. User/session auth already present in Management state (`veil admin reset` or
   `veil admin set --username admin --password ...`).

It also requires native TLS and authenticated metrics. Plain public HTTP is
refused before the server opens its socket. The
`--unsafe-allow-public-http` / `VEIL_UNSAFE_ALLOW_PUBLIC_HTTP=true` escape
hatch exists only for controlled recovery and sends credentials without
transport protection.

Caddy mode is evaluated as public exposure even though Veil itself listens on
loopback. It requires a configured Panel user and authenticated metrics before
startup. The API token remains optional for browser-only Caddy deployments and
is required for token-authenticated automation.

The bearer token is compared in constant time and accepted via either header:

```
Authorization: Bearer <token>
X-Veil-Token: <token>
```

- **Always set a token** for direct public listeners and API clients. Empty
  token mode is only acceptable for loopback-only deployments fronted by SSH.
- Browser access uses `/api/auth/login`, an HTTP-only `veil_session` cookie,
  CSRF headers for mutating requests, and admin/viewer RBAC. Viewer sessions
  cannot mutate state. Sessions have a 30-minute idle timeout and a 24-hour
  absolute lifetime.
- Session metadata survives Panel restarts in
  `/var/lib/veil/sessions.json`. Only SHA-256 hashes of the session and CSRF
  bearer values are persisted; raw values remain browser-side. Password and
  role changes, user deletion, and explicit administrator revocation invalidate
  affected sessions.
- First-run setup is available only on a loopback `local` listener with no
  users. It is not served through Caddy or a direct listener.
- Candidate configuration is checked against live ports, DNS, runtime
  binaries, and managed units. The server repeats validation immediately
  before every settings, Inbound, WARP, or apply mutation; failed checks return
  `422` without changing state.
- `/metrics` has an independent policy: `--metrics-access auto` (default),
  `authenticated`, or `public`. `public` is rejected on non-loopback Panel
  listeners.
- Tokens should be ≥ 32 random bytes. Rotate by restarting `veil serve` with a
  new token and updating any API clients.
- The Panel sets baseline security headers on every response
  (`X-Content-Type-Options`, `X-Frame-Options: DENY`, `Referrer-Policy`,
  `Cross-Origin-Resource-Policy: same-origin`, HSTS when served over TLS) and
  suppresses the `Server` header.

## 3. Credentials at rest

- Management state secrets (Inbound passwords, Client profile credentials, WARP
  secrets) are encrypted at rest by the State store and redacted in API
  responses and previews per the Credential disclosure rules.
- The `/etc/veil` directory is created `0750` and should be owned by root (or
  the dedicated `veil` user in container deployments). Do not loosen these
  permissions.
- Never commit `veil.env` or generated config to version control. Treat backups
  produced by the Backup lifecycle as sensitive — they contain managed material.

## 4. Encrypted backups and key lifecycle

- CLI archive creation requires encryption unless the operator explicitly uses
  `--allow-unencrypted`. Plain archives contain both state and its decryption
  key and should not be used for routine operations.
- Native packages ship hardened `veil-backup.service` and
  `veil-backup.timer` units. Enable them with
  `veil backup schedule enable --passphrase-file <path>`.
- The scheduled passphrase is stored at `/etc/veil/backup.passphrase` with
  mode `0600`; the Panel reads it server-side and never returns it to the
  browser.
- Store retained archives off-host and keep the passphrase in a separate
  security domain. Local retention does not protect against host loss.
- Run `veil backup verify` or `veil backup restore --check-only` before every
  restore. Veil validates checksums, the state/key pair, and Management schema
  compatibility before writing.
- Restore creates `.pre-restore-*` safety copies. Keep them until service
  health and all enabled Inbounds are verified.
- Rotate state encryption with `veil admin rotate-key` after suspected key
  exposure or migration from an untrusted host, then export a new archive.

See [Disaster Recovery And Key Lifecycle](disaster-recovery.md) for the full
runbook.

## 5. Panel audit history

- Authentication, setup, user/session administration, configuration mutation,
  apply, and service actions are written as compact JSONL records under
  `/var/lib/veil/audit/panel.jsonl` when the default state path is used.
- The recorder rotates at 5 MiB and retains five generations
  (`panel.jsonl.1` through `panel.jsonl.5`). Files and directories are
  owner-only by default.
- Detail keys associated with passwords, tokens, cookies, CSRF values,
  authorization headers, API keys, and private keys are recursively replaced
  with `[REDACTED]`.
- Administrators can inspect up to 500 records per request with
  `GET /api/audit?limit=100`; use the returned `nextBefore` timestamp to page
  backward. Viewer sessions are denied.

## 6. Supply-chain integrity

- **Release binaries** are verified by the installer against `checksums.txt`,
  with a uniqueness guard that rejects a forged duplicate asset line before
  running `sha256sum -c`.
- **Signed releases:** release artifacts (`checksums.txt`, SBOM) are signed with
  [cosign](https://docs.sigstore.dev/) keyless signing. Verify before trusting:

  ```sh
  cosign verify-blob \
    --bundle checksums.txt.bundle \
    --certificate-identity-regexp 'https://github.com/mikkelchokolate/Veil/.*' \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    checksums.txt
  ```

- **SBOM:** every release ships an SPDX SBOM (`veil.sbom.spdx.json`) describing
  all module dependencies. Use it to audit for known-vulnerable components.
- **Provenance attestations:** release archives and native packages are covered
  by GitHub build provenance attestations. Verify them with:

  ```sh
  gh attestation verify dist/veil_linux_amd64.tar.gz \
    --repo mikkelchokolate/Veil
  ```

- **Pinned CI actions and dependency automation:** GitHub Actions are pinned by
  commit SHA, CodeQL scans Go code, and Dependabot watches Go modules, Docker,
  and workflow dependencies.
- **Routing source material** (route-dat files) is checksum-verified before it
  is staged, so a tampered routing mirror cannot inject rules.

## 7. Host and runtime hardening

- **Shipped systemd hardening is the baseline.** Packaged units include
  `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp`,
  restricted address families, native system-call architecture, and explicit
  `ReadWritePaths` for Veil-managed state. Treat local drop-ins as further
  tightening, not as the first hardening layer.
- **Panel privilege boundary.** The Panel unit runs as `User=veil` and
  `Group=veil`, with empty `CapabilityBoundingSet` and `AmbientCapabilities`.
  It can read the root-owned configuration under `/etc/veil` and write only
  Panel-owned state, staging, updates, sessions, and audit data under
  `/var/lib/veil`.
- **Privileged helper.** Root-only operations are exposed by
  `veil-helper.socket` at `/run/veil/helper.sock`. The socket is
  `root:veil 0660`; the helper verifies the caller with `SO_PEERCRED`, accepts
  only an allowlisted protocol over `AF_UNIX`, and runs with
  `PrivateNetwork=true`. It has no TCP or UDP listener.
- **Root operation allowlist.** The helper may promote or restore generated
  configuration, control allowlisted Managed systemd units, read bounded
  journald output, create/verify/restore encrypted backups, rotate the state
  key, apply predefined firewall material, install a checksum-verified staged
  Veil binary, and restart the Panel. Arbitrary commands, paths, units, and
  shell input are rejected.
- **Writable paths.** The Panel owns `/var/lib/veil/state.json`,
  `/var/lib/veil/sessions.json`, `/var/lib/veil/audit`, staging, and updates.
  Root retains `/etc/veil/state.key`, backup passphrases, live generated
  configuration, systemd units, and migration safety copies under
  `/var/lib/veil/migration-backups`.
- **Containers run as a dedicated user.** The container image runs as the
  non-root `veil` user and relies on mounted state directories. A rootless
  container can provide local/read-only administration and staging, but full
  live host orchestration requires the bare-metal helper. Do not mount the host
  systemd tree into the container as a replacement for that boundary.
- **Firewall.** Only expose the ports you actually use. The Panel-facing
  firewall material plans rules from enabled Inbounds and Panel access — review
  `GET /api/firewall` output and apply the minimum.
- **Keep the toolchain current.** CI builds and releases on the latest Go and
  fails on any `govulncheck` finding, so staying on tagged releases keeps you
  ahead of stdlib and dependency CVEs.
- **Capability envelope.** Protocol runtime units that may bind privileged
  ports keep only `CAP_NET_BIND_SERVICE`. The Panel and helper units ship with
  empty capability sets; helper operations rely on root UID plus explicit
  systemd filesystem and syscall restrictions instead of ambient capabilities.

Check the boundary after installation:

```bash
systemctl status veil.service veil-helper.socket veil-helper.service
systemctl show veil.service -p User -p Group -p CapabilityBoundingSet
systemctl show veil-helper.service -p PrivateNetwork -p RestrictAddressFamilies
stat -c '%U:%G %a %n' /run/veil/helper.sock
```

If an ownership migration must be reversed, stop the Panel and helper, restore
the root-owned originals from `/var/lib/veil/migration-backups`, then run
`sudo veil repair --yes` before starting the units again.

## 8. Updates and rollback

- Update with `veil update`, which verifies release checksums, swaps the binary
  atomically, restarts the managed unit, health-checks the Panel, and can roll
  back during staged restart checks.
- Use `veil rollback list` / `veil rollback restore` to recover prior managed
  material, and review the append-only JSONL **Audit log** for install, repair,
  rollback, and backup outcomes.

## Reporting

Security issues should be reported privately — see [`SECURITY.md`](../SECURITY.md).
