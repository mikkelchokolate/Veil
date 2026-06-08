# Changelog

All notable changes to Veil will be documented in this file.

## Unreleased

## [v0.6.16] - 2026-06-08

### Fixed

- Enabling WARP is now robust against partial or stale form data. Re-enabling
  could still produce a non-functional config when the submitted payload was
  missing non-secret fields (peer public key, local address, reserved):
  - WARP update now preserves provisioned fields from stored state when the
    request omits them, so a re-enable keeps the existing account instead of
    wiping its peer/address details.
  - Auto-registration now triggers whenever the toggle is on but the effective
    config is incomplete (missing key, peer, or address) — not only when the key
    is empty — so flipping the toggle always yields a working WARP, and a config
    left half-populated by an earlier bug self-heals on the next enable.

## [v0.6.15] - 2026-06-08

### Fixed

- Turning WARP back on now works again. Re-enabling could leave WARP "on" with an
  empty, non-functional config:
  - The auto-register check inspected the raw request, so a UI re-sending the
    `[REDACTED]` key placeholder skipped registration; if the stored key had been
    cleared, resolving the placeholder produced an empty key and WARP came up with
    no credentials. Enable now resolves the placeholder against the stored key
    first and provisions a fresh account whenever the effective key is empty,
    repopulating every field from the registration.
  - Apply now also `enable`s `veil-warp.service` when WARP is on, so it survives a
    reboot. Disable (v0.6.14) leaves the unit disabled, and a plain restart alone
    would not re-enable it.

## [v0.6.14] - 2026-06-08

### Fixed

- Turning WARP off now actually stops it. Disabling WARP and applying tears the
  egress down: the live `sing-box/warp.json` is removed and `veil-warp.service`
  is stopped and disabled, so traffic stops routing through WARP.
  - The panel runs as an unprivileged user and cannot read the root-owned live
    `sing-box` directory, so its orphan scan never saw `warp.json`. WARP teardown
    is now driven from desired state: when WARP is disabled and the unit is still
    running, apply removes the artifact (which maps to `veil-warp.service`) so the
    existing stop-and-disable path runs. Gating on the running unit keeps the
    teardown to exactly once and leaves applies that never used WARP untouched.
  - `veil-warp.service` stays in the managed-unit set even while WARP is
    disabled, so apply is permitted to query its status and stop/disable it.

### Fixed

- WARP now actually starts and applies, completing one-click WARP (v0.6.12 only
  provisioned the account):
  - Apply **restarts** the WARP unit instead of reloading it. On first enable the
    unit is inactive, where `systemctl reload` fails outright; and the unit's
    `ExecReload` only validates, so a reload never applied a new config anyway.
  - The WARP unit now permits the `AF_NETLINK` socket family. sing-box's
    WireGuard endpoint subscribes to route updates over netlink; without it
    sing-box died at startup with "subscribe route updates: address family not
    supported by protocol".
  - Apply no longer aborts when the panel cannot read a root-owned generated
    subdirectory while scanning for orphaned configs — permission errors there
    are skipped (best-effort cleanup) instead of failing the whole apply.

## [v0.6.12] - 2026-06-07

### Added

- One-click WARP. Enabling WARP now provisions a free Cloudflare WARP account
  automatically — Veil generates a WireGuard keypair, registers the public key
  with the Cloudflare WARP API, and stores the returned peer key, endpoint,
  interface addresses, and reserved bytes. No private key or license to enter:
  flip the toggle and it works. The enable now persists (it previously failed
  validation with an empty key and silently reverted), and a WARP routing rule
  is added automatically.

## [v0.6.11] - 2026-06-07

### Fixed

- WARP no longer fails on current sing-box. The generated config used the legacy
  `wireguard` outbound with `local_address` (rejected by sing-box >= 1.11, which
  models WireGuard as an `endpoint`) and inline `geoip`/`geosite` route rules
  (removed in sing-box 1.12). The config now emits a `wireguard` endpoint
  (`address`/`peers`/`public_key`/`allowed_ips`), routes everything through WARP
  by default (`final: warp`), maps `geoip:private` to `ip_is_private`, and turns
  country `geoip`/`geosite` matches into remote `rule_set` references downloaded
  via the direct outbound. Validated against sing-box v1.13.

### Verified

- Reviewed each protocol runtime against current upstream: Hysteria2 (v2.9),
  NaiveProxy/Caddy (v2.11 with forwardproxy), and Mieru/mita (v3.33) configs
  still load/validate cleanly; only WARP/sing-box needed updating.

## [v0.6.10] - 2026-06-07

### Fixed

- Client-link QR codes now display. The panel renders the QR PNG from a `blob:`
  object URL, but the Content-Security-Policy `img-src` directive omitted `blob:`,
  so the browser blocked the image even though `/api/client-links/qr` returned it
  successfully. Added `blob:` to `img-src`.

## [v0.6.9] - 2026-06-07

### Fixed

- Mieru client links now include an importable `mierus://` share URI. Previously
  the link only carried a JSON config with an empty `uri`, so QR generation
  failed with "uri is required" and Mieru was excluded from URI subscriptions.
  The URI is validated against `mieru import config` / `mieru explain config`.

### Tests

- Raised apply-pipeline coverage substantially (`internal/applyflow` 19% → 95%):
  the apply workflow's confirm/plan/validation-gate/promote/services/health/
  rollback branches are now each exercised, so a regression in protocol
  deployment is caught. Added output-verification tests for the Mieru URI and
  client-link generation.

## [v0.6.8] - 2026-06-07

### Fixed

- Applying an inbound no longer fails for protocols without a standalone config
  checker. The apply gate treated a skipped validation as a failure, and the
  Hysteria2 (`hysteria server … --check`) and Mieru (`mieru check`) validation
  commands referenced flags/binaries that do not exist, so those inbounds could
  never go live. Hysteria2, Mieru and olcRTC now skip pre-stage syntax
  validation (relying on the post-restart health check, which rolls back), and a
  skipped validation no longer blocks the apply.
- Mieru's mita daemon now runs from an ephemeral `RuntimeDirectory` instead of a
  persistent `StateDirectory`, so each apply starts mita fresh and binds the
  inbound's configured port. Previously mita resumed a stale persisted config and
  kept serving the old port after a port change.
- A fresh install now defaults `settings.mode` to `server`. It was empty, and the
  settings validation requires a non-empty mode, so saving global settings (for
  example to set a `domain` for client connection links) failed with
  "panelListen and mode are required".

## [v0.6.7] - 2026-06-07

### Fixed

- Mieru inbounds now work with the current upstream server. Veil previously ran
  `mieru run -c <config>`, but mieru's server binary is `mita`, which is a daemon
  controlled over an RPC socket and has no `run -c` command — so Mieru could
  never start. The Mieru unit now launches `mita run` (using `MITA_CONFIG_FILE`
  and `MITA_UDS_PATH` under a root-owned `StateDirectory=mita`) and, once the RPC
  socket is ready, applies Veil's generated config and starts the proxy
  (`mita apply config … && mita start`). The runtime check and `veil doctor` now
  look for the `mita` server binary instead of the `mieru` client binary. Veil's
  generated `server_config.json` already matched mita's format. Install the
  server binary as `/usr/local/bin/mita` to use Mieru.

## [v0.6.6] - 2026-06-07

### Fixed

- The privileged helper now runs with the file capabilities it actually needs
  (`CAP_DAC_OVERRIDE`, `CAP_DAC_READ_SEARCH`, `CAP_CHOWN`, `CAP_FOWNER`) instead
  of an empty capability set. As root-with-no-capabilities it could not traverse
  the `veil`-owned `/var/lib/veil` or read `veil`-owned staging, so privileged
  operations over that tree failed — e.g. Backups returned
  `read backup directory: open /var/lib/veil/backups: permission denied`. The
  helper still has no network and no other capabilities.
- Service Status no longer breaks when managed systemd **template** units
  (`veil-caddy@.service`, `veil-hysteria2@.service`, `veil-olcrtc@.service`) are
  present. Templates cannot be queried with `systemctl show` (they have no
  instance), which previously failed the whole status batch with "Unit name … is
  neither a valid invocation ID nor unit name". Template units are now reported
  as inactive, and a single unit's query failure no longer aborts the rest.



### Fixed

- The panel TLS material in `/etc/veil/panel/` (self-signed cert and key used by
  `local`/`direct` access) is now given `root:veil` group-readable ownership
  during install, like the rest of `/etc/veil`. Previously `panel/tls.key`
  stayed `root:root 0600`, so the veil-owned Panel process could not read it and
  the service crash-looped with `open /etc/veil/panel/tls.key: permission
  denied` — the port never opened and the panel was unreachable
  (`ERR_CONNECTION_REFUSED`).
- The encryption-key loader now accepts `0640` (owner plus group-read) as a
  secure mode for `/etc/veil/state.key`, so a group-scoped veil service can read
  a root-owned key without the loader trying — and failing — to re-chmod it to
  `0600` on the read-only `/etc/veil` mount. This removes the spurious "error
  loading encryption key … read-only file system" startup log.

## [v0.6.4] - 2026-06-07

### Changed

- `veil uninstall` now removes configuration and state in `/etc/veil` and
  `/var/lib/veil` by default, so a reinstall starts fresh with a new password and
  panel path. Pass `--keep-data` to preserve credentials and configuration across
  reinstalls; `--purge` remains accepted as an alias for the default full removal
  and always overrides `--keep-data`. The locked `veil` account is still preserved.

### Fixed

- The install credential summary now prints the panel URL with its secret web
  base path for local/direct access (e.g. `https://127.0.0.1:2096/a1b2c3d4e5f6/`)
  instead of the bare root, which returned `404` and made the panel look
  unreachable after install.
- Reinstalling over an existing but unreadable state (corrupted state file or a
  state key that no longer matches) now fails with recovery guidance instead of
  silently leaving a panel that cannot be logged into. Reinstalls that reuse
  existing credentials also print how to reset the password or wipe and start
  fresh.

## [v0.6.3] - 2026-06-05

### Fixed

- Missing or inaccessible privileged helper sockets are now reported as a
  repairable `503 Service Unavailable` with native-install recovery commands
  instead of exposing a raw Unix socket dial error.
- Panel error rendering now unwraps structured API error envelopes, so Update,
  Service Status, and Backups show the actionable message instead of raw JSON.

## [v0.6.1] - 2026-06-05

### Fixed

- Direct/local Panel setups without `settings.domain` no longer fail the
  client-links API. Host-based exports are omitted until an endpoint is
  configured, while domainless `olcRTC` links remain available.
- Native install and repair now write all dormant managed runtime systemd unit
  templates, including Hysteria2, olcRTC, Mieru, WARP, backup, and helper
  units, without starting runtime services before inbounds exist.

## [v0.6.0] - 2026-06-05

### Security

- Public Panel exposure is fail-closed across direct and Caddy modes: user
  authentication and protected metrics are required, while direct exposure
  additionally requires TLS and an API token.
- The HTTP Panel now runs as the locked `veil` user with no capabilities.
  Allowlisted host mutations are delegated to a peer-credential-checked,
  socket-activated root helper.
- Browser sessions persist only hashed cookie and CSRF material, enforce idle
  and absolute expiry, and are revoked when user authority changes.
- Authentication and mutation events are written to a structured, redacted,
  rotated audit log.

### Added

- Added a loopback-only first-run setup flow for creating the initial
  administrator and acknowledging backup responsibilities.
- Added encrypted backup verification, compatibility metadata, scheduled
  systemd backups, daily/weekly/monthly retention, Panel backup controls, and
  cross-version restore fixtures.
- Added real-time port, DNS, runtime, and managed-unit validation plus richer
  secret-free apply previews with file operations, affected units,
  interruption risk, and rollback availability.
- Added complete English and Russian Panel catalogs with per-user locale
  persistence and a session-only locale API.
- Added skip navigation, keyboard-contained dialogs, screen-reader status
  regions, reduced-motion support, and responsive mobile layouts.
- Added a generated Go SDK and contract tests for the OpenAPI specification.

### Changed

- OpenAPI now documents the complete session, CSRF, RBAC, locale, error, and
  privileged-helper contracts with request/response schemas and examples.
- Native packages ship hardened Panel/helper units, a protected helper socket,
  scoped ownership migration, and permission-boundary tests by default.
- Viewer users may update only their own display locale; all management
  mutations remain admin-only.

### Fixed

- Backup restore jobs publish a terminal success state only after the matching
  audit record has been finalized.
- Panel HTML injects active CSRF material before placeholder cleanup, restoring
  cookie-session mutations such as persisted locale changes.
- Russian localization now covers dynamic operator actions and stable
  validation issue messages in addition to the static Panel shell.
- Installs targeting an alternative systemd directory now stage files without
  creating accounts, changing ownership, or invoking systemctl on the build
  host.
- Privileged backup archive names reject both Unix and Windows path separators
  consistently on every host platform.

### Migration Notes

- Native-package upgrades migrate Panel state to the `veil` account and enable
  `veil-helper.socket`; review custom filesystem ownership and systemd
  overrides before upgrading.
- Existing users default to English until they select another Panel language.
- Direct public listeners must provide TLS, API-token auth, user/session auth,
  and authenticated metrics or the server refuses to start.

## [v0.5.0] — 2026-06-04

### Security

- Public Panel listeners are now fail-closed unless both API token auth and user/session auth are configured.
- Added browser session authentication, CSRF protection, admin/viewer RBAC, session revocation, and first-admin setup support.
- Added independent `/metrics` access policy controls and blocked public metrics on public Panel listeners.
- Shipped hardened systemd units by default with constrained capabilities and read/write paths.
- Hardened Docker Compose defaults, Docker runtime paths, and release supply-chain verification.
- Bumped `golang.org/x/net`, `golang.org/x/crypto`, and `golang.org/x/text` and raised the Go toolchain to 1.25.11 to clear known vulnerabilities in called code.

### Fixed

- Mieru generated config now rejects duplicate usernames across aggregated enabled Inbounds (including generated client profiles) instead of emitting a config the `mieru` server would reject at load time.
- Fixed key rotation rollback atomicity and propagated secret decryption failures during state load.
- Fixed cross-platform end-to-end graceful shutdown behavior and dev-mode auth disablement.
- Fixed olcRTC auth credential encryption/redaction coverage.

### Added

- Added native `.deb`, `.rpm`, and `.apk` packages (built with nfpm) attached to releases, installing the Panel binary and managed systemd units.
- Added an SPDX SBOM, keyless cosign signatures, and GitHub provenance attestations for release artifacts.
- Added CodeQL and Dependabot configuration, pinned GitHub Actions, and a `make verify-release` release gate.
- Added an OpenAPI 3.1 specification for the Panel HTTP API at `docs/openapi.yaml`, including validation in CI/release checks.
- Added a hardening guide, disaster recovery guide, troubleshooting guide, install guide, roadmap, documentation index, and release verification instructions.
- Added encrypted and plaintext backup create/restore commands, backup archive compatibility tests, and restore coverage.
- Added atomic state key rotation with rollback safety and decryption-error propagation.
- Added state schema migrations for long-lived management state compatibility.
- Added Panel UX controls for client exports, local QR rendering, user/session management, token rotation guidance, viewer role behavior, safe apply preview, and DNS/TLS/firewall/service warnings.
- Added NaiveProxy/Caddy multi-instance support with per-Inbound Caddyfiles and managed runtime catalog coverage.
- Added port collision and WARP port warning validation.
- Added a black-box end-to-end test suite (`test/e2e`, behind the `e2e` build tag) that compiles the real `veil` binary, runs `serve` over a live socket, and verifies health/readiness, bearer-auth gating, the full inbound→client-link→apply flow with on-disk generated config, duplicate-username rejection, state persistence across restarts, graceful shutdown, and the `config validate`/`version`/`doctor` CLIs.
- Expanded CI and release quality gates to enforce gofmt, `go mod tidy`, staticcheck, govulncheck, shellcheck, the race detector, and coverage reporting.
- Added end-to-end Mieru Inbounds, generated config, client artifacts, managed runtime metadata, firewall planning, apply validation, live promotion, repair, and uninstall coverage.
- Added optional HTTPS Panel access through Caddy with a random Web base path.
- Added self-signed Panel TLS for direct/local Panel access without Caddy.
- Added protocol runtime provisioning reporting in Apply plans and Panel Apply preview.

### Changed

- Interactive `veil install` now asks for Panel exposure mode (`local`, `direct`, or `caddy`) and random/custom Panel port selection.
- The curl installer no longer forces `--panel-access local` during interactive installs, allowing the CLI wizard to collect exposure mode and port choices.
- Direct/local Panel docs now treat random Panel ports as the safe interactive default instead of assuming `2096`.
- NaiveProxy/Caddy runtime management now uses `veil-caddy@<inbound>.service` style instances instead of a single aggregate Caddy runtime.
- Apply, repair, uninstall, status, service actions, and Panel previews now understand multi-instance managed runtimes.
- Orphaned managed systemd instances are stopped and disabled during apply/promotion cleanup.
- Docker builds and release verification now run through stronger CI gates and container publishing checks.
- The Go module path is now `github.com/mikkelchokolate/Veil`.
- Windows/default path handling and interactive passphrase prompting were made more portable.
- Veil install is Panel-only; protocols are configured later as Panel Inbounds.
- Veil install writes systemd units by default, defaults direct/local Panel access to port 2096, requires root in the curl installer only for real applies, avoids binary install side effects during curl `--dry-run`, validates missing installer option values before side effects, requires exactly one exact checksum asset match for release assets, preserves interactive prompts through `/dev/tty`, and runs daemon-reload/enable/restart for installed Panel units.
- Veil install and repair render systemd units and install plans with the resolved Caddy binary path when Panel Caddy access is used, and packaged systemd unit templates now match the renderer including `veil.env`, WARP, and Mieru units.
- Veil status and staged update health checks prefer local generated Panel TLS and trust loopback self-signed Panel certificates.
- Panel Caddy access now renders, plans, validates, repairs, and exposes firewall rules end-to-end even when no NaiveProxy Inbound exists, while rejecting non-Caddy TCP/443 runtime conflicts.
- Docker runtime directories are writable by the non-root `veil` user, Docker health checks use explicit HTTP for the default non-TLS server, and local/CI release gates now include a build plus installer script syntax/help checks.
- Veil repair writes systemd units by default, reloads systemd after repairing unit files, preserves existing Panel secrets/TLS material, and repairs Panel Caddy access files from either existing env or encrypted Panel state.
- Veil uninstall removes managed systemd unit files, honors custom install/config/systemd paths, and runs daemon-reload after uninstall; the curl uninstaller forwards those paths, validates missing option values before side effects, and allows non-root dry-run previews.
- Removed legacy protocol stack and shared proxy port install inputs instead of hiding or translating them; dead install-time protocol artifact, install client link, stack policy, shared port planning, port availability, shared-port firewall planning, protocol binary acquisition, binary download/repair, protocol checksum input, and Naive Caddy build Modules were removed from the installer.
- RURecommendedProfile, Veil install/repair workflow inputs, the Settings Interface, RU-recommended profile preview Interface, Panel settings actions, Client link responses, and the curl installer parser no longer carry legacy protocol stack/install fields; strict JSON Interfaces now reject removed `stack` input instead of accepting it for compatibility.
- NaiveProxy client links use the `naive+https://` scheme.
- Protocol capabilities now drive Inbound options, Generated config set rendering, Client link delivery, Apply actions, managed runtimes, and repair planning.

### Migration Notes

- Public `veil serve` listeners now require both an API token and at least one configured Panel user/session auth account.
- Interactive installs may choose a random high Panel port; use the printed Panel URL or SSH tunnel command instead of assuming `2096`.
- NaiveProxy/Caddy managed runtime instances now use `veil-caddy@<inbound>.service`; stale instances are cleaned up during apply/promotion.
- Go consumers importing the module must use `github.com/mikkelchokolate/Veil`.
- Operators should verify backup restore and key rotation procedures before upgrading production state.

## [v0.3.16] — 2026-05-06

### Changed

- Extracted Management snapshot construction and secret transforms into its own Module
- Extracted Atomic file writing into its own Module

## [v0.3.15] — 2026-05-06

### Changed

- Extracted Config validation command mapping and execution into its own Module
- Extracted Management config rendering and WARP routing projection into its own Module
- Extracted Firewall rule projection from Settings and Inbounds into its own Module

## [v0.3.14] — 2026-05-06

### Changed

- Extracted Promoted service reload mapping and execution into its own Module

## [v0.3.13] — 2026-05-06

### Changed

- Extracted Live config promotion, backup, live-path mapping, and rollback into its own Module

## [v0.3.12] — 2026-05-06

### Changed

- Extracted Apply stage writer for staged plan, snapshot, generated configs, route-dat files, and validation

## [v0.3.11] — 2026-05-06

### Changed

- Extracted Settings management validation/redaction/save ordering from HTTP handlers
- Extracted WARP management redaction/defaults/save ordering from HTTP handlers
- Extracted Apply history storage, filtering, capping, and stage selection into its own Module

## [v0.3.10] — 2026-05-06

### Changed

- Extracted Routing rule management mutation and persistence ordering from HTTP handlers

## [v0.3.9] — 2026-05-06

### Changed

- Extracted route-dat download/checksum logic into its own Module
- Extracted Routing preset definitions from the management HTTP implementation

## [v0.3.8] — 2026-05-06

### Changed

- Extracted Client subscription formatting and query validation from HTTP handlers
- Extracted Apply plan building into a pure Module with render-validation callbacks

## [v0.3.7] — 2026-05-06

### Changed

- Deepened the Panel Module by extracting Inbound and Client profile form slices from the giant HTML string
- Centralized State store secret policy so encrypted fields are transformed through one Module
- Centralized Client profile credential projection for Client links and Generated config set rendering
- Added an Apply workflow state seam with a fake Adapter test
- Extracted Inbound management mutation/persistence ordering from HTTP handlers

## [v0.3.6] — 2026-05-06

### Fixed

- Release CI flake in corrupted ciphertext regression test by tampering decoded ciphertext bytes instead of randomly replacing a base64 character

## [v0.3.5] — 2026-05-06

### Added

- `ClientProfileCatalog` Module for Client profile password generation, preservation, and enabled filtering
- Simple Panel controls for adding Client profiles without hand-writing JSON
- Renderer support for multiple NaiveProxy and Hysteria2 users from Client profiles

### Changed

- Client links and Generated config set now use Client profiles end-to-end
- Client profile passwords are encrypted at rest by the State store

## [v0.3.4] — 2026-05-06

### Added

- Client profiles attached to Inbounds for 3x-ui-style multiple users on one listener
- Client links now generate one URI per enabled Client profile
- Generated Caddy and Hysteria2 configs now render multiple users from Client profiles
- Client profile passwords are generated by the backend when omitted and encrypted at rest
- Panel Inbound form now includes a Client profiles JSON editor

### Changed

- Clarified domain language: **Client profile** is distinct from **Inbound**

## [v0.3.3] — 2026-05-06

### Fixed

- Apply plan now rejects multiple enabled Inbounds of the same protocol instead of allowing Generated config set overwrite
- Documented the current Generated config set limitation for multiple same-protocol Inbounds in `CONTEXT.md`

## [v0.3.2] — 2026-05-06

### Added

- `CONTEXT.md` with Veil domain language for architecture reviews and future changes
- `BuildRURecommendedInstall` workflow Module to preserve panel-port-before-profile ordering
- Docker examples for Web base path and auto-TLS options

### Changed

- CLI install now delegates ru-recommended profile ordering to the installer Module
- Local `make release-check` now passes on a clean tree

## [v0.3.1] — 2026-05-06

### Added

- Per-inbound passwords for NaiveProxy and Hysteria2 client links
- Backend password generation for new inbounds without a provided password
- Password preservation on inbound update when no replacement password is provided
- Inbound passwords encrypted at rest in `state.json`
- `make release-check` for vet, tests, diff check, and dirty-tree guard

### Changed

- Extracted `ClientLinksBuilder`, `InboundCatalog`, `StateStore`, `ApplyWorkflow`, and `GeneratedConfigSet` Modules for better locality and testability
- Generated configs now use per-inbound passwords when present, falling back to global protocol passwords
- Install credential disclosure is centralized in a small summary Module

## [v0.3.0] — 2026-05-06

### Added

- Auto-generated random web base path for the panel (`/a1b2c3d4e5f6/`) — hides the panel from casual scanners
- Panel served via Caddy reverse proxy with automatic Let's Encrypt HTTPS — no separate port or manual TLS config
- `VEIL_WEB_BASE_PATH` written to `/etc/veil/veil.env` during install, picked up automatically by `veil serve`
- Interactive install now shows the full panel URL (`https://domain.com/a1b2c3d4e5f6/`) and credentials (username, passwords)
- Interactive install confirmation prompt (`Apply install plan? [y/N]`) instead of requiring `--yes`
- Simplified user-facing documentation

## [v0.2.1] — 2026-05-05

### Added

- `veil uninstall` CLI command — safely removes Veil panel, services, and configuration
  - `--dry-run` previews the removal plan
  - `--yes` confirms the operation
  - Stops and disables veil, veil-naive, veil-hysteria2, veil-warp services
  - Removes /etc/veil, /var/lib/veil, and /usr/local/bin/veil
- `scripts/uninstall.sh` — curl-installable uninstaller script

## [v0.2.0] — 2026-05-05

### Added

- `GET /api/version` — server version and runtime info (Go version, OS/arch, name)
- `POST /api/tools/dns-lookup` — DNS resolution from the server (`{"hostname":"..."}` → addresses, CNAME)
- `GET /api/firewall` — UFW firewall status and planned rules based on inbounds and settings
- `POST /api/tools/ping` — ICMP ping from the server (`{"host":"...","count":3}` → min/avg/max/stddev)
- Web panel cards for Version, Firewall, DNS lookup, and Ping
- Rate limiting for `/api/tools/dns-lookup` (10 req/min, burst 3) and `/api/tools/ping` (5 req/min, burst 2)

## [v0.1.0] — initial release

- CLI: install, repair, rollback, serve, update, status, config, doctor, version
- API: system, tls, network, connections, processes, disk, services, logs, settings, inbounds, routing, WARP, client-links, apply, speedtest, metrics
- Web panel with full management UI
- Secrets at rest encryption (AES-256-GCM)
- TLS 1.2+, rate limiting, security headers, auth tokens
- Backup/rollback with audit logging
- Self-update with checksum verification and staged rollback
- Docker deployment support
