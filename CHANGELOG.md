# Changelog

All notable changes to Veil will be documented in this file.

## Unreleased

### Fixed

- The release workflow now publishes a prerelease tag (for example
  `v1.2.3-rc.1`) as a GitHub prerelease with `--latest=false`, and a stable
  `vX.Y.Z` tag with `--latest`, using the same stable-semver test as the
  Docker `:latest` tag (#1148). Before, `gh release create` ran with no
  prerelease handling, so an RC became the Latest release and was what
  `releases/latest/download/install.sh`, `install.sh` without `--version`
  and `veil update` / panel Update installed on stable hosts (#1414).
- `POST /api/v1/users/me/totp/enroll` on an account that already has TOTP
  enabled now also requires `code`, a live and unused code from the current
  authenticator, which the call spends. Before, a session that had passed
  2FA could mint a new seed with no password and no code, confirm it, and so
  replace the owner's authenticator and recovery codes and revoke the
  owner's sessions, a stronger action than disabling TOTP, which already
  needs a live code. First-time enrollment is unchanged. A wrong code is
  throttled like other factor checks (#1413).
- Routing rule create/update (`POST /api/routing/rules`,
  `PUT /api/routing/rules/{name}`) and state import/rollback/`veil config
  validate` now reject an `outbound` other than exactly `direct`, `proxy` or
  `warp` with 400 / a validation error. Before, any non-empty value
  (`Direct`, ` warp`, `block`, typos) was saved and the next apply failed
  with `unsupported routing outbound`, blocking every later apply until the
  rule was fixed. Matching is case-sensitive, like the apply planner. Stored
  state that already holds such a rule still loads; fix or delete the rule
  through the API. Rolling back to a revision that holds such a rule, even
  a disabled one, is now refused with a validation error, as for an
  unparseable `match` (#1082), because enabling it later would block every
  apply (#1410).
- The SFTP backup destination now persists and uses the same trimmed
  host, user, remote directory, key path and pinned host key that
  validation checks. Before, a destination saved through the API with
  surrounding whitespace was accepted but stored verbatim, so it dialled
  `" example.com :22"`, authenticated as `" backup"`, and scoped uploads
  under the relative path `" /srv/veil /veil-node-…"` instead of the
  validated `/srv/veil`. Configs already on disk with such whitespace are
  normalized when loaded. Archives that such a config already uploaded
  stay under the old relative path in the SFTP user's home; they are no
  longer listed, fetched or pruned and must be moved or removed by hand. A
  pinned host key pasted with a trailing CRLF is now trimmed and accepted;
  an embedded `\r` is still rejected. Passwords and key passphrases are
  not trimmed (#1408).
- A panel IP certificate issuance whose `--installcert` step exits 0 but
  installs rejected material (missing, failing validation, or not replacing
  the previous cert) now drops the ACME enrollment it created, exactly like an
  `--issue` or `--installcert` exec failure (#1246 item 3). Before, acme.sh
  kept the enrollment and its reloadcmd for a cert Veil had rolled back.
  Enrollments that existed before the run are still preserved.
- The Caddy forward_auth user list for naive inbounds, including the
  revoked-client sentinel (#1098), is now derived in one place
  (`caddyassembly.NaiveUsers`). Before, the naiveproxy plugin re-derived the
  users over the render plan's copy, so the plan-level copy that install,
  repair, panel access and the apply-plan validator render from had no test
  of its own and could drift from the plugin (#1404 review follow-up). The
  rendered users are unchanged.
- The Caddy render plan now fails with a `panelListen` error when Caddy panel
  access has an unparseable or out-of-range `panelListen` port, and with a
  `panelPublicPort` error when the public port is outside 1-65535 (#1292
  finding 6). Before, the backend port became 0: a panel-only bind proxied to
  `127.0.0.1:0`, and a bind shared with naive silently dropped every panel
  route. `RenderHysteria2` also rejects listen ports above 65535. Settings
  validation already rejected these values, so valid installs are unaffected.

- A rollback no longer restores a mixed-case client binding `runtimeIdentity`
  (#1273 item 1). Identities are lowercase-only since #1111, and hysteria2
  reports lowercased usernames, so a mixed-case identity sent that binding's
  traffic to the unknown-identity bucket. The restored identity is now
  lowercased before it is written to the database and the new revision, and
  the result is checked with the same `ValidateRuntimeIdentity` rule the API
  uses. Older revisions that still hold mixed-case identities can be restored
  as before. A revision with two identities on one inbound that differ only by
  case is now rejected with 422, which matches the case-insensitive uniqueness
  index.
- A rollback that moves a client binding back to the client it had in the
  revision no longer replaces the binding's live credentials with the older
  revision credential (#1272 item 6). The binding row is still re-created,
  which drops its traffic counters as before, but its credential rows, including
  a rotation made after the revision and its revoked tombstones, are kept as
  they are. Bindings only change client through direct database edits, so this
  path was not reachable through the API or CLI.
- Client `email`, `groupId` and `notes` are now validated like the display
  name instead of being stored verbatim (#1272 item 7). `email` must be at most
  254 bytes and `groupId` at most 128 characters, both without control or format
  characters or surrounding whitespace. `notes` may be up to 4096 characters
  and keep tabs and line breaks, but refuses other control characters, invalid
  UTF-8 and bidirectional override/isolate marks. Values stored by earlier
  builds are kept: an update that leaves such a field unchanged (including
  enable/disable and bulk actions) still succeeds, and a rollback can restore
  them. Only a new value has to pass.
- Revisions committed through the CLI state path (`veil admin set`, install
  apply, repair, key rotation) now keep each client's `ipLimit` in their
  immutable snapshot. That path previously dropped it, so rolling back to such
  a revision restored every client as IP-unlimited while `deviceLimit`
  survived. Revisions committed before this fix still lack `ipLimit`, and a
  missing value there reads as "unlimited". After rolling back to one of them,
  re-check client IP limits.
- `veil admin set --username NAME` now applies the Panel username policy
  (3-64 characters, at most 64 UTF-8 bytes, letters, digits, `.`, `_`, `-`)
  when NAME would create a new account, the same policy first-run setup and
  `POST /api/users` already enforce. The CLI could previously create users
  such as `me`, which the `/api/v1/users/me/*` self-service routes shadow so an
  admin could never reset its second factor over the API, as well as names
  with `/`, spaces or newlines that no `/api/users/{username}` route can
  address. Updating an existing account by its exact name still works, so a
  legacy name stays recoverable from the CLI (#1266).
- Root-helper backup reads (archive inspection, decrypt, metadata digests,
  `state.json` schema probes, restore staging) no longer hang if a file under
  the backups tree is swapped for a FIFO between the check and the open. The
  open uses `O_NONBLOCK` and refuses anything that is not a regular file
  (FIFO, directory, device, socket) on the held descriptor (#1254).
- Source-built `main-<short SHA> (<full SHA>)` installations can update through
  the Panel or CLI without forcing when GitHub proves the running commit is
  included in the target release. The downloaded signed provenance is bound
  to that same target commit. Newer/diverged, unknown or unverifiable builds
  still require an explicit override; release downgrade protection is unchanged.
- Downloading a backup archive from the Panel now writes a `backup.download`
  audit record, like every other backup operation. Each attempt gets exactly one
  record with the archive name, the bytes sent and, if the download did not
  complete, the reason: the helper refused, the mutation fence was held, the
  archive identity drifted mid-stream, or the client disconnected.
- `veil helper serve` now prints a `warning:` on stderr for every `VEIL_*`
  variable in its environment that moves a root-helper policy path (state,
  `state.key`, staging/live roots, backup passphrase and root, SFTP config,
  status, known_hosts and install id) away from the packaged layout. A manual
  `sudo -E veil helper serve` with a poisoned shell env no longer retargets key
  generation and backups silently. The packaged unit's values do not warn.
  Values are quoted, and a `VEIL_BACKUP_PASSPHRASE` value that is not an
  absolute path, or that contains blanks, is redacted.

### Removed

- #1273 item 2: the unused `generatedconfig.InboundRenderer`
  (`NewInboundRenderer`, `RenderHysteria2Inbound`, `RenderOlcrtcInbound`) is
  gone. Production renders hysteria2 and olcRTC through the protocol plugins.
  The dead olcRTC path still made up a random key when the inbound had none,
  which the plugin refuses because the server key would then differ from the
  client export. Its hysteria2 path still rendered a revoked-sentinel account
  for an inbound whose clients were all disabled, while the plugin drops that
  inbound. One API test exercised that dead path and so passed for behaviour
  production never had. It now renders through the real management renderer:
  the hysteria2 inbound produces no config, and the naive Caddy config carries
  only the install-secret-keyed sentinel. The olcRTC resolver/transport
  regression test (#823) now runs against the plugin.

## [v0.7.3] - 2026-10-22

Faster panel-first installation, recovery controls that work without the web
Panel, stronger account and state protection, and more useful live traffic
and host diagnostics. This release includes the changes since v0.7.2, not
only the installation and CI optimizations.

### Highlights

- Install only what you use: protocol runtimes are acquired when an enabled
  Inbound is first applied; WARP acquires sing-box when enabled. Disabled
  Inbounds do not download runtimes. Caddy Panel access still acquires its
  required Caddy immediately.
- Run `sudo veil` or `sudo veil menu` for terminal management and recovery:
  service controls, logs, diagnostics, repair, credentials, backups, updates
  and selected runtime installation remain available when the web Panel is down.
- Follow real apply phases, including downloading, building, verifying,
  installing and service activation, with the runtime name and elapsed time.
- Add TOTP or WebAuthn/passkeys as a second factor for Panel login; manage
  encrypted off-host backups over SFTP.
- See live client presence, listeners, traffic history and managed-service
  processes. Traffic History requests and renders only the newest 60 points;
  stored history has a 90-day default retention and is not deleted by that
  display cap.

### Upgrade notes — read before updating

- Create and verify an encrypted backup with your current Veil version
  **before the first v0.7.3 start**. Keep the backup and its passphrase safely
  off the server. It must preserve the matching `state.json`, `veil.db` and
  `state.key`; copying only the executable is not a data backup.
- The first start migrates management state to **schema 6** and authenticates
  revision snapshots. Older binaries refuse that state. Downgrading requires
  the matching pre-upgrade data and binary, not just replacing the executable.
  `veil update --staged` can roll back a binary after a failed health check,
  but does not undo state or database migrations.
- Pre-upgrade historical revisions remain unsealed and cannot be used for
  apply rollback. If the applied revision lags the desired revision during
  migration, subscription feeds remain unavailable until a successful apply.
  Invalid legacy subscription-token seals may require token re-issuance;
  inspect startup warnings rather than assuming every legacy token migrated.
- A binary-only update does not refresh install-managed systemd units. Review
  `sudo veil repair --dry-run`, apply the required repairs, restart the helper
  and Panel, then check `veil doctor` and `veil status`. Apply once from the
  Panel to refresh generated runtime configuration and certificate routes.
  Custom-root installations must retain their own `--etc-dir`/`--var-dir`.
- First-use runtime downloads/builds require internet access. Verified managed
  binaries are reused only when provenance, pinned version and executable
  permissions match; stale or untracked binaries may be repaired. Preinstall
  selected runtimes with `sudo veil runtime install --only mieru,hysteria2`
  before an offline maintenance window. Cold Caddy/olcRTC source builds have a
  15-minute apply budget and have not been timed on a 1-vCPU/1-GB VPS.
- `VEIL_AUTO_TLS=yes` and `VEIL_AUTO_TLS=on` are no longer accepted; use a
  strict boolean such as `true` or `false`.
- Keep the direct IP-certificate opt-out (`VEIL_PANEL_LE_IP_CERT=0`) if you
  do not want automatic issuance during live Apply or periodic renewal.

See the [installation and upgrade guide](https://github.com/mikkelchokolate/Veil/blob/v0.7.3/docs/install.md)
and [known limitations](https://github.com/mikkelchokolate/Veil/blob/v0.7.3/docs/known-limitations.md)
before production rollout.

### Added

- Optional TOTP with recovery codes and WebAuthn/passkeys as a **second
  factor**, not passwordless login. Enrollment requires credential checks;
  replay protection, guess limits and last-factor removal guards apply.
- Encrypted SFTP backup upload, retention and fetch/restore, with remote
  status and host-key pinning/TOFU. Remote upload failures do not erase a
  successfully committed local backup.
- Per-client live device/IP limits for Hysteria2 HTTP authentication, honest
  protocol capability reporting, per-user Mieru traffic accounting, and live
  presence/listener/history views on the Traffic page.
- Managed-service process visibility and live per-process CPU usage on the
  System page, plus DNS reachability warnings in install, repair and doctor.
- Best-effort direct IP-certificate issuance after a successful live apply
  with service actions, and a Veil-owned renewal worker. Certificate failures
  are reported without pretending the successful configuration apply failed.

### Changed

- Panel installation no longer downloads every protocol runtime. Explicit
  `veil runtime install` still installs all runtimes; `--only` selects a subset.
  On-demand installation uses the fenced privileged helper and pinned,
  verified artifacts; download retries never bypass integrity checks.
- Client operations avoid repeated full-snapshot scans and redundant database
  queries, Inbound client previews are bounded, and abandoned list/history
  requests are cancelled. A 5,000-client fingerprint benchmark improved from
  about 878 ms to 17 ms in [#1363](https://github.com/mikkelchokolate/Veil/pull/1363);
  this is a focused benchmark, not a whole-Panel claim.
- CI shares measured shard timings, reuses validated build/runtime caches,
  avoids an accidental real Caddy build in a unit test, and reduces duplicate
  browser setup. The [final optimization CI run](https://github.com/mikkelchokolate/Veil/actions/runs/39773004034)
  took 10m20s wall-clock versus the measured [14m04s baseline](https://github.com/mikkelchokolate/Veil/actions/runs/39760774474);
  its slowest shard took 7m23s instead of 11m35s. Race,
  coverage, inventory, browser and cold-install gates remain enabled.
- Tagged releases reuse successful main CI and CodeQL for their exact source
  commit instead of repeating source tests, browser E2E and cold installs.
  Archive/package builds reuse the source- and content-verified frontend from
  that CI run; shipped-package tests, pushed-image execution checks, signing
  and provenance remain release gates.
- Hardened session revocation, subscription-token bindings, authenticated
  revision snapshots, promotion/restore journals, filesystem ownership and
  symlink handling, root tool lookup and Go build environments. Restricted
  egress, mutation validation and rollback now fail closed more consistently.
- Administrator passwords can be read from an interactive prompt, stdin or
  a file. Prefer `--password-stdin` or `--password-file` for automation;
  the legacy `--password` argument remains visible in the process list.
- #1263/#1334: `VEIL_AUTO_TLS` is parsed strictly by `veil serve`
  with `strconv.ParseBool` (`1`, `t`, `T`, `true`, `TRUE`, `True` and the
  matching false forms). Values such as `yes` or
  `on` used to enable auto-TLS and now stop startup with
  `VEIL_AUTO_TLS must be a boolean` — check Docker/compose environments.
  `VEIL_ACME_INSECURE` instead treats any non-true value as false, so a
  typo can never disable ACME TLS verification.

### Removed

- #1292: the unused Caddyfile text-template renderers
  (`renderer.RenderNaiveCaddyfile`, `renderer.RenderPanelCaddyfile` and the
  `generatedconfig` naive/panel wrappers that called them) are gone. They
  interpolated credentials, the domain, the WARP upstream and the panel base
  path into Caddyfile text unquoted, so a value with whitespace or Caddyfile
  tokens could inject directives. Nothing in production called them: Veil
  deploys one consolidated Caddy JSON config through the admin API, and that
  path is unchanged. Its public-TLS-only guard (no internal-CA issuer for a
  naive domain) now has a JSON-path test.

### Fixed

- #1369: when a batched `systemctl restart` fails during install or apply,
  the error now includes the tail of systemctl's own output (capped at
  2 KiB), so it names the unit that failed instead of a bare
  `exit status 1`. Flags no longer add to the restart timeout budget. A
  descendant that keeps systemctl's output open can no longer hold the
  command past its timeout, and it doesn't turn a successful restart into
  a failure.
- Direct-mode access URLs retain the configured port, secret base path and
  correct IPv6 brackets; failed public-IP detection is reported honestly.
  Installation restarts dependent services together instead of repeatedly
  bouncing a healthy Panel into systemd's start limit.
- Diagnostics reads the actual firewall response shape instead of crashing
  with `(t.data ?? []).map is not a function`; generated API clients and the
  Go SDK match the response contract.
- Authentication-status refreshes are cancelled and invalidated when their
  provider unmounts, preventing stale responses from changing session/CSRF
  state or updating a page that has already closed.
- Helper-side runtime probes work with PrivateTmp and a strict umask without
  weakening isolation. Runtime publication/rollback preserves executable
  permissions, managed Caddy is pinned and verified, routine applies avoid
  repeated proven version probes, and interrupted asset bodies retry within
  one bounded attempt budget.
- #1292: WARP auto-registration no longer trusts the Cloudflare
  registration response blindly. The peer endpoint it returns is advisory:
  it is used only when it is `host:port` with a valid port and a host inside
  `cloudflareclient.com`, and only when the `PUT /api/warp` request left the
  endpoint unset or at the stock `engage.cloudflareclient.com:2408`. An
  endpoint the operator set is kept, and a foreign host or IP literal falls
  back to the stock endpoint, so a tampered response cannot repoint WARP
  egress. A `PUT /api/warp` that omits `endpoint` now keeps the stored
  endpoint instead of resetting it to the stock one; sending `"endpoint":""`
  (the panel's cleared field) still resets it. The peer public key must be
  a base64 32-byte key, the interface addresses must be bare IPv4/IPv6
  addresses of the right family, and `client_id` must decode to 3 reserved
  bytes. A malformed response now fails the PUT with a
  `WARP registration failed: ...` error naming the field, instead of being
  persisted and later rejected by `sing-box check`.
- #1292: `veil repair` (and install's repair plan) no longer reads managed
  files through a path that can be swapped after its type check. The
  content comparison now reads through an `O_NOFOLLOW|O_NONBLOCK`
  descriptor and judges the inode it pins, so a leaf swapped for a symlink,
  a FIFO or a different file between the check and the read is reported as
  drifted and atomically replaced. Before, a planted symlink to a file with
  the expected content was reported clean, and a FIFO hung the plan
  indefinitely. The read is bounded to the expected size. A managed-file
  list that names the same path twice with different content, mode or owner
  now fails the plan instead of rewriting the file on every repair;
  identical duplicates are collapsed.
- #1288: Mieru (`veil-mieru`) now resolves names on hosts where no
  `/etc/resolv.conf` nameserver is reachable under the protocol units'
  kernel egress filter (AWS VPC `172.31.0.2`, `169.254.169.253/.254`,
  Alibaba `100.100.2.136`, LXC/home-lab `10.x`/`192.168.x`, a local dnsmasq
  on `127.0.0.1`). mita has no resolver setting, so the unit now runs
  `veil egress-resolv-conf` as root before mita starts. On such hosts it
  writes `/run/veil-mieru/resolv.conf` naming Cloudflare's public resolvers
  (`1.1.1.1`, `1.0.0.1`, or `2606:4700:4700::1111`/`::1001` on hosts that
  route only IPv6 to Cloudflare), and the unit bind-mounts it read-only over
  `/etc/resolv.conf` in its own mount namespace. This is plain DNS on port
  53, because mita's resolver cannot speak DoH. Other hosts get no file and
  keep the host resolver unchanged. A missing `/etc/resolv.conf` is left
  alone. The check runs at every unit start, so no re-apply is needed after
  the host DNS changes. Hosts that install the packaged unit pick this up on
  upgrade; installs with custom paths pick it up on the next `veil install`
  or `veil repair`. `veil doctor`, `veil install` and `veil repair` report
  the fallback nameservers in their DNS warning, and `veil doctor --json`
  has a new `mieruNameservers` field. `veil update` does not re-render
  install-managed units, so when the loaded `veil-mieru.service` predates
  the hook, `veil doctor` keeps saying Mieru domain lookups fail, sets
  `mieruUnitOutdated`, and points at `veil repair`.
- #1288: root `veil install` and `veil repair` now print the same
  protocol-unit DNS warning as `veil doctor`. The install capability report
  has a new `egress-dns` row: `ok` when a `/etc/resolv.conf` nameserver is
  reachable under the units' kernel egress filter, and `warn` with the
  doctor's remedy when none is, when `resolv.conf` is missing, or when it
  cannot be read. `warn` is advisory and never blocks the install. `veil
  repair` (including `--dry-run`) prints the warning after its plan. Both
  checks run only on Linux; non-root installs, which manage no protocol
  units, skip it.
- #1288: `veil doctor` now checks DNS for the protocol units. It reads
  `/etc/resolv.conf` with the same rules the Hysteria2 and WARP renderers
  use and reports whether any nameserver is reachable under the units'
  kernel egress filter (the systemd-resolved stub or a public address). When
  none is (AWS VPC `172.31.0.2`, link-local metadata resolvers, CGNAT,
  `10.x`/`192.168.x`, a local dnsmasq on `127.0.0.1`, or no `resolv.conf`),
  it prints a warning: Hysteria2 and WARP pin Cloudflare DoH at apply, but
  Mieru domain lookups fail. An unreadable `resolv.conf` is reported as
  unknown. The check is a warning only and does not change `Ready`; `--json`
  carries it as `egressDNS`. It runs only on Linux.
- #1288: the WARP sing-box bridge (`veil-warp`) now gets the same DNS fix as
  Hysteria2. On hosts where no `/etc/resolv.conf` nameserver is reachable under
  the unit's kernel egress filter, the rendered `warp.json` pins a Cloudflare
  DNS-over-HTTPS server (`1.1.1.1:443`, or the IPv6 address on IPv6-only
  hosts, where the resolver also prefers AAAA records), dialed directly and
  used as `route.default_domain_resolver`. Before
  this, sing-box could not resolve the WARP peer hostname, so the tunnel never
  came up, and client domains and rule-set downloads failed too. Hosts on the
  systemd-resolved stub or a public nameserver render the same config as
  before. Mieru's separate fallback is described above.
- #1257 (F3): promotion journal recovery and promotion restore no longer
  recreate a symlink at a managed destination from a `wasSymlink` /
  `oldLinkTarget` record. The link target was checked only against a digest
  stored in the same record, so a forged journal or manifest could make the
  helper create a root-owned link to any target at a managed path, which
  normal promote refuses to publish over. The helper never publishes symlinks
  there, so a rolled-back or restored symlink record now leaves the
  destination absent. A restore reports it under removed artifacts.
  Operators who deliberately symlinked a managed path: rolling back a
  promote that removed that link no longer brings it back.

- #1257 (F2): the privileged helper now recovers an interrupted promotion
  journal only while holding `.promotion.lock`. Before, `promote`, promotion
  restore and journal recovery replayed or rolled back the on-disk journal
  first and took the lock afterwards, so a second concurrent request could
  roll back a transaction another request was still publishing under the
  lock (restoring its safety copies over freshly published artifacts and
  deleting its journal), leaving a torn mix at managed destinations. A
  restore now also reads its manifest under the same lock hold.

- #1355: `POST /api/apply/rollback` now supersedes the live quota-enforcement
  record of a client restored with a different quota plan (quota bytes, reset
  policy or reset boundary) than its live row, as a live reset-policy or
  boundary change already does.
  Before, the record bound to the old plan stayed active, so enforcement kept
  using the old values (for example a pending or backing-off "depleted"
  target after the restored quota was raised).
  A rollback that restores the same plan leaves the record untouched.

- #1351: `POST /api/apply/rollback` now also refuses with 422 a `quotaBytes`
  restored onto a client whose enabled binding lands on an inbound that
  cannot enforce it in the restored configuration (disabled, or a protocol
  without quota enforcement), the #1173 rule every client write already
  applies. Before, the rollback committed the quota silently inert: the
  runtime never rejected the depleted client, and the next quota edit on that
  client was refused with 400. As for device/IP limits, bindings already
  disabled live do not count. `PUT /api/inbounds/{name}` now checks the
  updated inbound instead of only the transition: an edit that leaves the
  inbound disabled or on a protocol without quota enforcement is refused with
  409 while an enabled quota-bound binding is attached, even if the inbound
  was already incapable (stale restore or legacy data). Re-enabling it on
  hysteria2 is never blocked.
- #1350: `POST /api/apply/rollback` to a revision whose client has no quota
  no longer carries the live `depleted` flag onto the restored client.
  Depletion is derived quota state, so restoring "no quota" is now handled
  like clearing the quota through the API: the flag is dropped and the
  client's pending quota enforcement target is superseded. Before, the
  rollback committed `depleted=true` with `quotaBytes=null` (a state every
  writer refuses) to the clients table and the new immutable revision.
  A client whose restored revision still has a quota keeps its live
  depletion as before.
- #1270: `POST /api/apply/rollback` now runs the restored client rows
  through the same domain rules as every live writer, and refuses with 422
  (`selected immutable revision is invalid: ...`) instead of committing
  them. That covers `deviceLimit`/`ipLimit` below 1, a negative or
  over-range `quotaBytes`, an unknown `quotaResetPolicy`, depletion without a
  quota, a non-positive `expiresAt` and malformed names. Before, the storage
  triggers turned some of these into an opaque 500 and let the rest commit.
  The rollback also refuses device/IP limits on a client whose enabled
  binding lands on an inbound that does not enforce them in the restored
  configuration (the #1224 rule). Bindings already disabled live do not
  count. A revision taken before those rules were tightened can no longer
  be rolled back to as-is; fix the client in the current revision instead.
  Only clients the rollback actually restores are checked: a client deleted
  since the revision was taken is not resurrected, so its stale values do
  not block the rollback.
- #1346: revoking or rotating a subscription token now destroys its sealed
  issuance binding (`token_ciphertext` is cleared in the same `UPDATE` that
  sets `revoked_at`). Before, the binding stayed valid, so a veil.db writer
  without `/etc/veil/state.key` could re-activate a revoked or rotated-out
  token with `UPDATE subscription_tokens SET revoked_at=NULL`. Rows revoked
  by an earlier build are cleared on the next panel start. `enabled` stays a
  reversible plain column by design.
- #1280: `revision_snapshots` rows are now authenticated. Each row carries a
  `payload_mac` (migration v32): an HMAC-SHA256 keyed from
  `/etc/veil/state.key` over its revision, creation time, state-file digest
  and exact payload. The `/s/{token}` feed, the apply render path, rollback
  and startup only read rows whose MAC verifies. A veil.db writer without the
  state key can therefore no longer rewrite the applied row to point
  subscribers at its own endpoints with their real credentials, nor plant a
  desired row (attacker-authored or plaintext credentials, `ve1:garbage`)
  for the privileged apply. A planted row at a revision the panel is about to
  commit is refused, not adopted. State-key rotation re-seals rows under the
  new key, and only those that verify under the old key; it refuses to
  rotate while an edited sealed row is present. Upgrade note: rows written
  before this release have no MAC. On the first start, the panel seals the
  desired row only after proving it against `state.json` (the same
  projection check as the v4 digest binding) and checking that every
  credential in it is a valid state-key ciphertext. If that check fails,
  startup stops, as for a digest mismatch. It then rewrites `state.json` at
  schema 6, re-binding the sealed row to the new bytes (an install with no
  desired revision yet gets the rewrite too); schema 6 records, outside
  veil.db, that the one-time legacy pass is over. From then on an
  unsealed desired row (a blanked `payload_mac`, or a planted higher desired
  revision) stops startup instead of being re-sealed. Every `state.json`
  this release writes is schema 6, so older Veil builds refuse it (newer
  schema); downgrading after the first start needs a pre-upgrade backup.
  Other pre-upgrade rows stay unsealed, so rollback to a revision older than
  the upgrade is refused. If the applied revision lagged the desired one at
  upgrade time, the public feed is unavailable until the next successful
  apply. Residual: a veil.db writer who acts before the first start of this
  release, or before the first start after restoring a pre-upgrade backup
  (restore republishes that backup's schema-5 `state.json`, which re-opens
  the legacy pass on purpose so its desired row can be sealed), can still
  shape the client, binding and credential-assignment sections of the one
  legacy desired row (only its `state.json` part and the
  validity of its credential ciphertexts are proven). After that start, the
  snapshots themselves cannot be edited, but the live client tables are not
  authenticated: an edit there is copied into the next revision a mutation
  commits and seals, as the live gate already reads it.
- #1265: session revocation by `veil admin set`, `admin reset` and
  `admin rotate-key` now reaches the running panel. The panel's SIGHUP
  reload re-reads `sessions.json` and its journal, so sessions the CLI
  revoked stop working right away instead of living on for up to 24 hours.
  The CLI records the revocation as a durable journal intent (`revoke_username`,
  or the new `revoke_all`) that covers every session created before it ran,
  so a session record the panel writes before it handles the signal cannot
  bring a revoked session back. `admin reset` and `rotate-key` no longer
  delete `sessions.json`: the panel used to answer a missing snapshot by
  rewriting every in-memory session to disk, which undid the invalidation
  for good. Files the root CLI writes in the session store are handed to the
  store directory's owner, so the unprivileged panel can still read them.
  Every panel-side rewrite of the whole store (snapshot save, journal
  compaction) first applies the revocations journaled on disk, and the CSRF
  refresh after a panel restart journals an upsert instead of rewriting the
  store, so the revocation also holds when the panel rewrites sessions
  before it is signalled. The panel and the CLI take an exclusive advisory
  lock on `sessions.json.lock` (owned by the store directory's owner) around
  every whole-store rewrite, journal append and store load, so a CLI command
  that coincides with a panel rewrite can no longer have its revocation
  dropped between the rewrite's journal read and its journal removal. The
  lock uses `flock` and applies on Unix hosts; elsewhere the store is not
  shared with a root CLI. The service-identity migration now also re-owns
  `sessions.json.journal` and `sessions.json.lock`. When `veil.service` is not active, the CLI now
  prints a warning that no running panel was reloaded, instead of reporting
  success without one. A
  `revoke_cancel` record now retracts only the intent it was paired with.
  Downgrading to an older build makes it quarantine a journal that holds a
  `revoke_all` record, which logs everyone out once.

- #1281: a `subscription_tokens` row no longer authorizes itself. The panel
  seals each token's id, client, expiry and plaintext into
  `token_ciphertext` under a token-only subkey of the state key, and
  `/s/{token}` plus token reveal only accept a row whose sealed binding
  matches it. Without `/etc/veil/state.key`, a veil.db writer can no longer
  plant a row for a chosen token, rewrite a row's `token_hash`, re-point a
  token at another client, rename it, or extend its expiry. Copying another
  `ve1:` value, such as a client's own credential, into a token row no
  longer works either. On the first start after upgrading, tokens issued
  earlier, which sealed the bare token under the master key, are re-sealed
  into bindings once, and a `migration_markers` row records this. Bare
  seals planted after that are refused, are not re-sealed on later starts
  and are dropped by state-key rotation. Because the marker lives in
  veil.db, the one-shot pass also refuses to run while any sealed binding
  or any live row it cannot re-seal is present, and it never re-seals a
  value that is a stored client credential. Upgrade note: a live token row
  with an empty or invalid seal also blocks that pass, so pre-upgrade tokens
  are then not re-sealed; the panel logs a warning, and those tokens must be
  re-issued. Residual: a veil.db writer who deletes the marker, deletes or
  revokes every live token row, and deletes the copied credential, then
  waits for a panel restart, can still get one known non-credential
  master-key plaintext re-sealed. Rows without a `ve1:` ciphertext, which
  only pre-0.7.0 development builds could produce, are refused too; rotate
  them.
- #1288: on hosts where no `/etc/resolv.conf` nameserver is reachable under
  the Hysteria2 unit's kernel egress filter (AWS/GCP/OCI non-Ubuntu images,
  LXC, local dnsmasq), the rendered Hysteria2 config now pins a Cloudflare
  DNS-over-HTTPS resolver: `1.1.1.1:443`, or the IPv6 address on IPv6-only
  hosts. Before this, every domain request and domain ACL rule failed while
  the service stayed active. Hosts on the systemd-resolved stub or a public
  nameserver keep their system resolver. The separate Mieru and WARP fixes
  are described above.
- #1334: `veil uninstall` no longer warns that `VEIL_CADDY_STATE_DIR` /
  `VEIL_MITA_STATE_DIR` retarget removal when `--caddy-state-dir` /
  `--mita-state-dir` was passed explicitly, nor for a value that only
  differs from the packaged path by a trailing slash.
- #622: package postinstall no longer sweeps every symlink under
  /etc/veil/www on each upgrade — the legacy /var/lib/veil/www migration
  copies only regular files and directories, skips source symlinks, and
  never touches operator-created destination entries.
- #623: package postinstall and `veil install` host migration re-own an
  existing /var/lib/mita to the dedicated veil-mita identity — systemd never
  re-owns an existing StateDirectory, so a veil-owned mita tree left the
  veil-mieru.service unit unable to write its state.
- #625/#639/#650: a custom `--etc-dir`/`--var-dir` install on a packaged
  host now writes install drop-ins for the protocol units
  (veil-hysteria2@, veil-olcrtc@, veil-warp, veil-mieru) too, and every
  drop-in clears the vendor list directives before assigning custom values
  — EnvironmentFile, ReadOnlyPaths, ReadWritePaths, and InaccessiblePaths
  accumulate across fragments, so the packaged defaults no longer leak in
  beside the custom layout.
- #626: the custom-path veil-backup.service drop-in resets the packaged
  `ConditionPathExists=/etc/veil/backup.passphrase` so the oneshot is not
  silently skipped on custom `--etc-dir` installs.
- #627: `veil backup schedule enable` writes its passphrase drop-in with
  the effective unit's state/key/output paths instead of stomping the
  ExecStart back onto the packaged defaults, and resolves the passphrase
  destination from the configured `--passphrase-file` when the operator
  does not pass `--passphrase-path`.
- #642: `veil uninstall` and scripts/uninstall.sh now remove every managed
  unit's drop-in directory (veil.service.d, veil-helper.service.d,
  veil-caddy.service.d, the protocol units, and more) instead of only
  veil-backup.service.d — stale 10-veil-install.conf overrides no longer
  merge into the next package install.

### Install and verify

Fresh installation of this exact release:

```bash
curl -fsSL https://github.com/mikkelchokolate/Veil/releases/download/v0.7.3/install.sh | sh -s -- --version v0.7.3
```

Linux amd64 and arm64 archives and native `.deb`, `.rpm` and `.apk` packages
are published with SHA-256 checksums, an SPDX SBOM, SLSA provenance, Sigstore
bundles and GitHub artifact attestations. The bootstrap verifies release
evidence before its privileged handoff. Container images are published as
`ghcr.io/mikkelchokolate/veil:0.7.3` and `latest`; rootless containers do not
replace the host's privileged helper for systemd/firewall orchestration.

[Full commit comparison](https://github.com/mikkelchokolate/Veil/compare/v0.7.2...v0.7.3)

## [v0.7.2] - 2026-09-18

Patch release: clean-host install acceptance now gates every merge, and the
audit it enabled produced installer/runtime fixes for Caddy-mode installs,
generated-config permissions, controlled ACME CAs, state-key rotation, and
protocol credential/export contracts.

### Changed

- New required CI gate install-acceptance (amd64 and arm64): a real
  `veil install` on a clean VM, firewall/panel-readiness checks, a first
  inbound per protocol through the management API, controlled ACME
  issuance via Pebble, a Caddy-mode public-route leg, and
  reboot/reinstall persistence. Runtime downloads retry on transient
  registry errors.
- web-dependencies group bump (9 updates).

### Fixed

- `veil install --panel-access caddy` on an existing install re-rendered
  the previous mode's Caddy JSON, so caddy started with no panel server,
  bound nothing on :443, and the installer still reported success for an
  unreachable URL. The snapshot render now uses the new profile's panel
  settings, and install verifies the public route (unit active, :443
  listener, TLS answer, DNS-vs-public-IP advisory) before printing
  credentials.
- Generated protocol configs (/etc/veil/generated/**) and panel TLS
  material are readable by the veil-proxy runtime user; Hysteria2/Mieru/
  olcRTC units no longer crash-loop on config permission denied after
  install, which previously stalled synchronous API applies.
- VEIL_ACME_CA_URL/VEIL_ACME_INSECURE propagate into acme.sh issuance, and
  VEIL_ACME_CA_URL/VEIL_ACME_CA_ROOT configure the Caddy ACME issuer
  (ca, trusted_roots_pem_files) persisted via veil.env — enabling
  controlled/private ACME CAs.
- #307/#310/#312: rollback restores only the promoted artifacts, stops and
  disables units whose just-created configs were removed, persists the
  attempted-operation diagnostics on recovery-pending transitions, and
  inbound create sends an Idempotency-Key so timed-out requests replay
  safely.
- #296/#297/#302: GOROOT resolves through the selected Go binary, the
  toolchain cache publishes by atomic rename and rejects partial
  extractions, and rate-limited runtime fetches fall back to the
  expanded-assets page.
- #311/#122: one credential-byte contract for the Mieru fallback password
  across render/export/policy; olcRTC Autofill persists the effective
  encryption key; PerClientCredentialEnforcer separates link rendering
  from runtime enforcement.
- #174/#166/#155/#197/#208: protocol log units resolve to live template
  instances, PATCH counts as mutating in the rate limiter, event/traffic
  SSE streams get dedicated limits with Retry-After, loopback/link-local
  ping and DNS targets are rejected, and IPv6 listeners are parsed from
  /proc/net/*6.
- #162: state key rotation re-encrypts every live AES-GCM blob
  (subscription tokens and durable secret envelopes), so Panel Reveal and
  in-window idempotency replays keep working after rotation.

- Fresh-host `veil install --panel-access caddy` aborted with
  `caddy list-modules failed: executable file not found in $PATH` because
  the installer probed Caddy capabilities before runtime install placed the
  binary. Panel Caddy JSON now renders when Caddy is missing; `veil runtime
  install` still installs Caddy afterwards.
- Helper `parseUFWStatus` treated IPv6 `(v6)` as the UFW action, so apply
  restore replayed invalid syntax, rolled back live UDP/TCP rules, and the
  panel later marked `finalization_pending` as applied. IPv6 status twins
  are skipped on restore; install-time SSH/ACME comments are kept; Caddy is
  enabled after a successful load/reload; `finalization_pending` stays in
  recovery instead of succeeding.
- ACME tls-alpn challenge servers on TCP 443 now pin `h1`/`h2` so HTTP/3
  cannot steal UDP 443 from Hysteria2.
- Live Caddy JSON includes Hysteria2 inbound domains in ACME subjects when
  NaiveProxy is also enabled.
- WARP `ru-blocked` rule-sets fetch published SagerNet `geoip-ru` /
  `geosite-geolocation-ru` artifacts instead of missing `*-ru-blocked.srs`.
- Hysteria2 no longer revives the inbound password after every client
  profile is disabled.
- veil-helper.service can write `/run/ufw.lock` under ProtectSystem=strict.

## [v0.7.1] - 2026-09-12

Patch release: open-issue sweep from v0.7.0, runtime/toolchain bumps, and
Hysteria2 stall fixes for GUI clients.

### Changed

- Protocol runtime pins: Hysteria2 `app/v2.12.2`, Mieru/mita `v3.36.1`,
  WARP/sing-box `v1.14.0`, and olcRTC `189d16c093c4`. NaiveProxy remains on
  Caddy `v2.11.4` with the klzgrad forwardproxy fork.
- Panel toolchain: Go `1.27.1`, Node.js `26.8.2`, pnpm `12.4.1`,
  Playwright `1.63.0`, Alpine `3.24`, and current Go modules plus Panel SPA
  dependencies.

### Fixed

- Open GitHub issues #49–#69, #71–#90, and #94–#107: backup/rollback safety,
  installer ACME/UFW, subscriptions, idempotency, traffic observation,
  clients, live validation, staged updates, and Panel UI races.
- Hysteria2 inbounds ignore GUI-client advertised bandwidth and keep QUIC
  idle/keepalive windows that do not stall long-lived sessions.

## [v0.7.0] - 2026-08-23

Architecture rework of the Veil management plane: durable apply, normalized
Clients, React Panel, backup/restore, protocol plugins, and isolated CI.

### Changed

- Protocol runtime pins now track current supported upstreams: Hysteria2
  `app/v2.12.1`, Mieru/mita `v3.36.0`, WARP/sing-box `v1.13.19`, and olcRTC
  `f616f57bb3a9`. NaiveProxy remains on Caddy `v2.11.4` with the klzgrad
  forwardproxy fork (already latest). `veil runtime install` and CI install
  the same checksum-verified artifacts.
- Panel toolchain and libraries: Go `1.27.0`, Node.js `26.7.0`, pnpm
  `11.22.0`, Playwright `1.62.1`, and current Go modules plus the Panel SPA
  `web/` and `test/browser` dependency trees.
- README is now operator-focused: Panel GIF, Quick Start, Inbounds, and the
  complete CLI catalog. Development, Docker, and CI detail lives in `docs/`.

### Added

- `scripts/install-main.sh` installs the current `main` commit from source:
  `curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install-main.sh | sh`.
  It is the HEAD equivalent of the tagged-release `install.sh` bootstrap and is
  not cosign-signed.
- `veil help` prints the operator command catalog, including nested
  subcommands (`veil help admin`, `veil backup schedule`, and the rest of the
  tree). `veil help <command>` still shows flags for that command.
- Hysteria2 Inbounds can now serve a real ACME certificate for a domain that
  is not the Panel or a NaiveProxy Inbound. A Hysteria2-only domain switches
  from `tls-alpn-01` to the HTTP-01 challenge and gets a dedicated ACME
  challenge listener on TCP :80, so you can point any domain that resolves to
  the host at a Hysteria2 Inbound and obtain a trusted certificate without
  sharing the Panel/Naive Caddy listener. A Hysteria2 Inbound that reuses the
  Panel's or a NaiveProxy Inbound's domain keeps `tls-alpn-01` and reuses the
  certificate Caddy already manages for that domain. Requires TCP :80 to be
  free (or already served by Caddy) and the domain to resolve to the host; a
  Hysteria2-only domain conflicting on :80 produces a warning rather than
  blocking the apply.

### Fixed

- Tagged GitHub releases now build the Panel SPA (`web/dist`) before `go vet`
  and `go build`, so the published binary embeds the UI. The previous release
  path compiled the Go module without `make web` and failed `//go:embed all:dist`.
- Release quality e2e now creates the `veil` system user before root
  protocol data-path tests, and release browser-e2e uses the helper-backed CI
  suite instead of a helper-less loopback panel.
- The privileged helper (`veil-helper.service`) can now write to `/etc/ufw`,
  so firewall synchronization from the Panel (`/api/apply`) works in
  production. Previously the helper ran as root but was confined by
  `ProtectSystem=strict` without `/etc/ufw` in `ReadWritePaths`, causing
  `ufw allow ...` to fail with "'/etc/ufw/user.rules' is not writable".
- Protocol inbounds no longer fail to start with systemd status `203/EXEC`
  ("Failed to locate executable"). Veil previously wrote managed units that
  invoked `/usr/local/bin/{caddy,hysteria,mita,sing-box,olcrtc}` but never
  installed those runtime binaries, so every protocol (NaiveProxy, Hysteria2,
  Mieru, olcRTC, and WARP) failed to launch. `veil install` now provisions the
  runtimes automatically after the Panel is installed. (#20)
- Room generation is now served under `POST /api/protocols/{protocol}/room`
  (was `/api/{protocol}/room`) so OpenAPI paths resolve unambiguously; the
  legacy server-rendered panel uses the same endpoint.
- Rotating the state-encryption key now advances the desired revision and
  runs the same apply-job envelope as other mutations, so the Panel header
  does not stay Pending with no job.
- The Panel no longer cancels an in-flight apply when the browser request
  times out; the helper finishes the job and the UI polls it.
- Attaching a client to an inbound with an empty credential now generates
  that credential once and returns it in the mutation envelope.
- olcRTC apply now waits for the managed unit to stay active before the
  job is marked succeeded.
- Backup restore and state-key rotation can create sibling temp files next
  to `/etc/veil/state.key`. The helper no longer marks `/etc/veil` read-only
  while allowing only the key file itself, which failed with EROFS under
  `ProtectSystem=strict`.
- Panel Caddy now advertises only `h1`/`h2` on the Panel HTTPS port so HTTP/3
  does not occupy UDP 443 and block Hysteria2 Inbounds on that port.
- Creating a Hysteria2 Inbound without an explicit port now defaults to UDP
  443 instead of failing inbound validation.
- Live validation no longer reports privileged ports as in use when the
  panel process lacks `CAP_NET_BIND_SERVICE`. A bind that fails with EACCES
  now inspects `/proc/net` occupancy, so Hysteria2 can share UDP 443 with
  Panel Caddy on TCP 443.
- `veil uninstall` now stops loaded template instances (`veil-hysteria2@*`,
  `veil-olcrtc@*`) instead of trying to stop the template unit name itself.
- Fresh installs now grant the panel group-read on generated Caddy JSON
  (`0640 root:veil`, directory `0750`) so the first inbound apply can hash
  the live artifact instead of failing with permission denied.
- Fresh installs now write `/etc/veil/backup.passphrase`, and Panel backup
  create generates that file when it is missing, so Create backup works
  without a separate `veil backup schedule enable`. A too-short passphrase
  returns a public 503 instead of a generic privileged-operation error.
- The one-command installer now accepts checksum entries with either
  canonical asset names or the legacy `./` prefix, while release manifests
  are generated with canonical names. This fixes
  `Expected exactly one checksum ... got 0`.
- Root-driven installs no longer preserve the GitHub Actions runner UID
  while extracting the verified archive, so the secure binary handoff
  accepts the downloaded regular file without weakening its ownership
  checks.
- Normalized runtime credentials are now used consistently by global client
  links and subscriptions, so NaiveProxy links authenticate correctly.
- NaiveProxy now uses the configured WARP SOCKS outbound, while routing
  rules marked `direct` continue to use the server's original network path.
- Hysteria2 WARP ACL output now uses a valid outbound identifier.
- Durable apply now reports failed revisions as errors, waits for newly
  started services to become stable, and removes obsolete managed systemd
  wants.
- `veil config validate` can validate encrypted olcRTC state snapshots with
  an explicit `--key-path` without creating or replacing encryption keys.
- `veil status` now honors `--web-base-path` and `VEIL_WEB_BASE_PATH`,
  allowing it to query Panels mounted below a secret path.
- Runtime provenance is now covered by the endpoint authorization policy
  instead of being rejected as an unknown API operation.

### Added

- Plugin-based protocol architecture. Each proxy protocol (NaiveProxy,
  Hysteria2, Mieru, olcRTC) is now implemented as a plugin in
  `internal/protocols/<protocol>` that supplies its own config renderer,
  runtime/systemd descriptors, validator, UI field schema, and client-link
  builder. A central `internal/protocols.Registry` discovers built-in plugins
  and drives the runtime catalog, generated-config set, live validation, apply
  plan, and inbound autofill.
- Dynamic Panel UI for inbound protocol fields. The Panel now fetches protocol
  field schemas from `GET /api/protocols` and renders inbound-specific inputs
  (username/password, masquerade URL, olcRTC provider/transport/room, etc.)
  generically. Values are stored in the new `protocolFields` map on `Settings`
  and `Inbound`, while legacy flat fields remain supported for backward
  compatibility.
- `GET /api/protocols` endpoint returning protocol metadata, transports, and
  field schemas for the Panel renderer. Documented in `docs/openapi.yaml` with
  new `ProtocolInfo`, `FieldSchema`, and `FieldOption` schemas.
- State migration from schema version 3 to 4. On load, legacy protocol-specific
  flat fields (`naiveUsername`, `hysteria2Password`, `olcrtcRoomID`, etc.) are
  automatically moved into `protocolFields` so existing states work with the
  plugin validators and dynamic UI without manual conversion.
- Direct Panel access (`--panel-access direct`) now automatically obtains a
  trusted Let's Encrypt IP certificate using the `shortlived` profile
  (3-day validity, auto-renewed by `acme.sh`). This eliminates the
  self-signed certificate warning and the `ERR_TOO_MANY_REDIRECTS` / HSTS
  loop some browsers show for bare-IP access. Port 80/tcp must be reachable
  from the internet during the HTTP-01 challenge; if issuance fails, Veil
  falls back to the previous self-signed certificate and prints a warning.
  Use `--le-ip-cert` / `--le-ip-cert-port` to control the behavior.
- Direct Panel access (`--panel-access direct`) now auto-fills the Settings
  `domain` field with the server's public IP when it is empty. Client links,
  subscriptions, and generated proxy configs therefore use the IP as the
  endpoint instead of returning an empty link list. `veil repair` also
  backfills the domain for existing direct-mode installations.
- New `veil runtime install` command (with `--only` and `--bin-dir` flags) that
  downloads `caddy`, `hysteria`, `mita`, and `sing-box` from their upstream
  GitHub releases — verifying published SHA-256/SHA-512 checksums — and builds
  `olcrtc` from source with the Go toolchain. Runtime acquisition is implemented
  in the new `internal/runtimeinstall` package and is non-fatal during install:
  a failed download is reported as a warning and can be retried later.
- `veil doctor` now reports `olcrtc` alongside the other optional runtime
  binaries.

## [v0.6.21] - 2026-06-09

### Fixed

- Auto-generated olcRTC rooms no longer reveal that a panel created them. The
  room URL previously used a `veil-<hex>` suffix; it now uses a natural
  Jitsi-style name (e.g. `https://meet.small-dm.ru/WittyWolvesWanderBravely`),
  built from random capitalised words exactly like the names the Jitsi UI
  suggests, so the link is indistinguishable from an ordinary meeting.

## [v0.6.20] - 2026-06-09

### Changed

- olcRTC inbounds now have a per-inbound room "Generate" button that is enabled
  only for providers that support automatic rooms. Jitsi rooms are created on
  join, so the button generates a fresh room URL; Telemost and WbStream require
  a room created on the service first, so the button is disabled for them (with
  a tooltip explaining why). Room generation is served by a new
  `POST /api/olcrtc/room` endpoint that enforces the same rule server-side
  (400 for providers that cannot auto-generate), so the panel and backend share
  one source of truth.
- The olcRTC inbound form now lists the real providers (jitsi, telemost,
  wbstream) and transports (datachannel, vp8channel, seichannel, videochannel)
  instead of placeholder values, and generates a valid Jitsi room URL instead
  of a bare name.

## [v0.6.19] - 2026-06-09

### Fixed

- olcRTC inbounds now produce a working configuration and are one-click. Several
  bugs made olcRTC unusable:
  - The systemd unit ran `olcrtc --config <file>`, but the binary takes the
    config path as a positional argument (`olcrtc <file>`); the `--config` flag
    was treated as the config path and the service never started. Fixed in both
    the rendered and packaged units.
  - The config validation command (`olcrtc --config <path> --check`) was unsafe
    — olcRTC has no offline check subcommand and would try to run as a tunnel —
    so it is removed; render-time YAML validity is relied on instead.
  - The rendered config put the server domain in `net.dns` (which must be a DNS
    resolver) and could set `net.transport` to the inbound's L4 transport
    (`udp`) instead of a WebRTC channel type. `net.dns` now defaults to a public
    resolver and `net.transport` defaults to `datachannel`.
  - Creating an olcRTC inbound now auto-provisions a working setup: provider
    `jitsi`, an auto-generated room URL (Jitsi rooms are created on join, so no
    manual room is needed), `datachannel` transport, and a 64-hex shared crypto
    key. Client links carry that same shared key.

## [v0.6.18] - 2026-06-09

### Changed

- Switching tabs now refreshes that tab's data automatically. Previously a
  change made in one tab (e.g. enabling WARP adds the `geosite:openai -> warp`
  routing rule) only appeared after a full page reload; the Routing Rules,
  WARP, Inbounds, Dashboard, Backups, and Users tabs now reload their data on
  every switch.
- System telemetry (CPU / memory / disk) now auto-refreshes once per second and
  is enabled by default on page load, so the dashboard gauges are always live.
  A toggle on the "System resources" card turns the auto-refresh off and on.

## [v0.6.17] - 2026-06-09

### Fixed

- The WARP on/off slider now actually turns WARP on and off. The toggle looked
  like an instant switch but was an inert checkbox: nothing happened until the
  user found and pressed the separate "Save WARP config" button, and even then
  the change was only saved, not applied. The result was that flipping the
  toggle appeared to do nothing, the state "reset" on page reload, and the WARP
  routing rule was never added or removed.
  - Flipping the slider now immediately saves the desired state (which adds or
    removes the `geosite:openai -> warp` routing rule) and applies it (which
    starts or stops `veil-warp.service`), so the switch behaves like a real
    one-click control and the change persists across reloads.
  - The "Save WARP config" button routes through the same save-then-apply path,
    so editing credentials and saving also takes effect immediately.
  - If the save is rejected the slider snaps back to its true state and the
    error stays visible in the console.

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
- Mieru's mita daemon now reads its config and RPC socket from an ephemeral
  `RuntimeDirectory`, so each apply starts mita fresh and binds the inbound's
  configured port; mita keeps its persistent `StateDirectory=mita` for daemon
  state. Previously mita resumed a stale persisted config and kept serving the
  old port after a port change.
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
