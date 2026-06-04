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

- **Prefer `local` + SSH tunnel.** `ssh -L 2096:127.0.0.1:2096 user@host` keeps
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

The bearer token is compared in constant time and accepted via either header:

```
Authorization: Bearer <token>
X-Veil-Token: <token>
```

- **Always set a token** for direct public listeners and API clients. Empty
  token mode is only acceptable for loopback-only deployments fronted by SSH.
- Browser access uses `/api/auth/login`, an HTTP-only `veil_session` cookie,
  CSRF headers for mutating requests, and admin/viewer RBAC. Viewer sessions
  cannot mutate state.
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

## 4. Supply-chain integrity

- **Release binaries** are verified by the installer against `checksums.txt`,
  with a uniqueness guard that rejects a forged duplicate asset line before
  running `sha256sum -c`.
- **Signed releases:** release artifacts (`checksums.txt`, SBOM) are signed with
  [cosign](https://docs.sigstore.dev/) keyless signing. Verify before trusting:

  ```sh
  cosign verify-blob \
    --certificate checksums.txt.pem \
    --signature checksums.txt.sig \
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

## 5. Host and runtime hardening

- **Shipped systemd hardening is the baseline.** Packaged units include
  `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp`,
  restricted address families, native system-call architecture, and explicit
  `ReadWritePaths` for Veil-managed state. Treat local drop-ins as further
  tightening, not as the first hardening layer.
- **Root surface.** On bare-metal systemd installs, the Panel process still
  needs root-equivalent access to write `/etc/veil`, write `/var/lib/veil`, and
  call `systemctl` for managed units. The privileged operations are: install
  and repair managed unit files, promote generated configs, restart/reload
  managed units, read bounded journald logs, and rotate the state key.
- **Containers run as a dedicated user.** The container image runs as the
  non-root `veil` user and relies on mounted state directories. Do not mount the
  host systemd tree read-write unless you are intentionally delegating host
  service orchestration to the container.
- **Firewall.** Only expose the ports you actually use. The Panel-facing
  firewall material plans rules from enabled Inbounds and Panel access — review
  `GET /api/firewall` output and apply the minimum.
- **Keep the toolchain current.** CI builds and releases on the latest Go and
  fails on any `govulncheck` finding, so staying on tagged releases keeps you
  ahead of stdlib and dependency CVEs.
- **Capability envelope.** Units bound to potentially privileged ports keep
  `CAP_NET_BIND_SERVICE` and drop broader ambient capabilities. If your local
  deployment does not bind low ports directly, you can remove that capability in
  a drop-in after validating apply, diagnostics, and restart flows.

## 6. Updates and rollback

- Update with `veil update`, which verifies release checksums, swaps the binary
  atomically, restarts the managed unit, health-checks the Panel, and can roll
  back during staged restart checks.
- Use `veil rollback list` / `veil rollback restore` to recover prior managed
  material, and review the append-only JSONL **Audit log** for install, repair,
  rollback, and backup outcomes.

## Reporting

Security issues should be reported privately — see [`SECURITY.md`](../SECURITY.md).
