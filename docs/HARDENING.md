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

All `/api/*` routes are gated by a bearer token when one is configured
(`authMiddleware`). The token is compared in constant time and accepted via
either header:

```
Authorization: Bearer <token>
X-Veil-Token: <token>
```

- **Always set a token** for `direct`/`caddy` modes. An empty token disables
  the gate and is only acceptable for loopback-only deployments fronted by SSH.
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
- **Routing source material** (route-dat files) is checksum-verified before it
  is staged, so a tampered routing mirror cannot inject rules.

## 5. Host and runtime hardening

- **Run as a dedicated user where possible.** The container image already runs
  as the non-root `veil` user. On bare-metal systemd installs, the Panel needs
  root to manage units; restrict shell access to the host accordingly.
- **Firewall.** Only expose the ports you actually use. The Panel-facing
  firewall material plans rules from enabled Inbounds and Panel access — review
  `GET /api/firewall` output and apply the minimum.
- **Keep the toolchain current.** CI builds and releases on the latest Go and
  fails on any `govulncheck` finding, so staying on tagged releases keeps you
  ahead of stdlib and dependency CVEs.
- **systemd unit hardening (optional).** For defense in depth you can layer a
  drop-in (`/etc/systemd/system/veil.service.d/hardening.conf`) adding
  `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ProtectHome=yes`,
  `PrivateTmp=yes`, and a `ReadWritePaths=/etc/veil`. Validate carefully — the
  Panel needs to write managed material and reload units.

## 6. Updates and rollback

- Update with `veil update`, which verifies release checksums, swaps the binary
  atomically, restarts the managed unit, health-checks the Panel, and can roll
  back during staged restart checks.
- Use `veil rollback list` / `veil rollback restore` to recover prior managed
  material, and review the append-only JSONL **Audit log** for install, repair,
  rollback, and backup outcomes.

## Reporting

Security issues should be reported privately — see [`SECURITY.md`](../SECURITY.md).
