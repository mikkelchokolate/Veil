# Veil Context

Veil is a control plane for installing, configuring, and operating NaiveProxy, Hysteria2, olcRTC, and Mieru through a panel and CLI. This context records the domain language used by the codebase and architecture reviews.

## Language

**Veil install**:
An interactive or scripted setup flow and orchestration Module that installs the Panel, chooses Panel access, generates Panel credentials, optionally renders Panel Caddy access, prints the Panel access summary through a CLI workflow package, validates prerequisites, and applies Panel managed material. Protocol runtimes are configured later as Panel Inbounds.
_Avoid_: bootstrap, setup wizard, protocol stack installer

**Host environment**:
The install-time host facts and validators: platform, architecture, domain/email validity, public IP detection, and DNS matching. Installer and Settings policy use it through thin Adapters.
_Avoid_: installer utilities, network helpers

**Firewall material**:
The firewall rule plan, firewall status probe, and Panel-facing firewall rule responses derived from Panel access and enabled Inbounds.
_Avoid_: ufw helpers, port list

**Veil update**:
The CLI flow that fetches a GitHub release, verifies checksums, extracts the Veil binary, swaps it atomically, restarts the Managed systemd unit, checks Panel health, and can roll back during staged restart checks.
_Avoid_: updater helpers, download command

**Veil version**:
The CLI flow that compares the current Veil version with the latest GitHub release and renders update guidance. The version CLI workflow package owns release fetch and comparison behavior; Cobra only adapts flags.
_Avoid_: version flag helper, release check glue

**Veil serve**:
The CLI flow that resolves listen address, auth token, state paths, TLS material, Auto TLS settings, and Panel Web base path before starting the HTTP Panel server.
_Avoid_: serve helpers, server command setup

**Veil status**:
The CLI flow that resolves a Panel listen address, fetches `/api/status`, handles generated local Panel TLS, and renders human or JSON Service status output. The status CLI workflow package owns fetch and rendering behavior; Cobra only adapts flags.
_Avoid_: status command HTTP helper, service list printer

**Veil doctor**:
The CLI readiness flow that checks required and optional host commands and renders a human or JSON readiness summary. The doctor CLI workflow package owns readiness and presentation rules; Cobra only adapts flags.
_Avoid_: doctor print helper, dependency checklist glue

**Veil uninstall**:
The CLI flow that previews and removes Veil managed units and binary material. By default it also removes configuration and state in `/etc/veil` and `/var/lib/veil` so a later install starts fresh with a new password and panel path; `--keep-data` preserves them, while `--purge` is an explicit alias for the default full removal that always overrides `--keep-data`. The locked `veil` account is always preserved. The uninstall CLI workflow package owns the plan, ordering, and concrete side-effect actions; Cobra only adapts flags and writers.
_Avoid_: remove command script, cleanup helper

**Veil rollback**:
The CLI flow that lists backups, restores backup files, cleans up backup records, and records rollback audit events. The rollback CLI workflow package owns backup lifecycle orchestration; Cobra only adapts flags.
_Avoid_: backup command helper, restore subcommand glue

**Veil repair**:
The CLI flow that previews and repairs Panel managed material, Generated config artifacts, Managed systemd units, backups, and repair audit events. The repair CLI workflow package owns profile selection, plan preview, dry-run, and apply ordering; Cobra only adapts flags and writers.
_Avoid_: repair command glue, reinstall-lite

**Panel**:
The browser UI and HTTP management surface used to operate Veil after install.
_Avoid_: dashboard, admin site

**Panel slice**:
A cohesive Panel feature area that owns its HTML slots, JavaScript actions, and event bindings together. Panel slices render through the Panel rendering package instead of each HTTP Adapter assembling HTML directly.
_Avoid_: UI component, widget

**Web base path**:
A random URL path prefix that hides the Panel behind Caddy and routes browser requests to Veil.
_Avoid_: secret route, slug

**Panel URL**:
The final HTTPS URL a user opens to reach the Panel, composed from domain and Web base path.
_Avoid_: dashboard link, admin URL

**Panel access**:
The way a user reaches the Panel and the package Module that turns that choice into validation, install profile material, Panel Caddy access material, Panel TLS material, and apply intent: direct HTTP listen address, local-only listen address for SSH tunneling, or HTTPS through Caddy. HTTP and installer code are Adapters over the package rules.
_Avoid_: dashboard exposure, admin binding

**Panel Caddy access**:
A Panel access mode where Caddy terminates HTTPS on a domain and reverse-proxies a Web base path to the local Panel port. It is not NaiveProxy and does not imply any protocol runtime.
_Avoid_: Naive Caddy stack, shared proxy port Panel

**Panel TLS**:
Self-signed HTTPS generated by the Panel access package for direct/local Panel access when Caddy is not used. It encrypts Panel traffic on the Panel port; Caddy mode terminates HTTPS in Caddy instead.
_Avoid_: protocol TLS, Naive TLS, Caddy replacement

**Panel IP certificate**:
A trusted Let's Encrypt certificate issued for the server's public IP in `direct` Panel access mode (`acmeip`): `shortlived` profile (3-day validity), SAN = IP address, no `CN`, auto-renewed by `acme.sh` standalone with a `reloadcmd` that restarts the Panel. Replaces the self-signed Panel TLS when issuance succeeds.
_Avoid_: IP cert, direct TLS cert

**Hysteria2 Inbound TLS**:
The ACME certificate path for a Hysteria2 Inbound assigned a domain. If the domain is already the Panel's or a NaiveProxy Inbound's domain, the Inbound reuses the Caddy-managed certificate via `tls-alpn-01`; a Hysteria2-only domain switches to the HTTP-01 challenge and gets a dedicated ACME challenge listener on TCP :80. Without a domain the Inbound uses a self-signed certificate.
_Avoid_: hysteria cert, QUIC TLS cert

**ACME challenge binding**:
The per-domain choice of ACME challenge and the listener that satisfies it, decided by the Caddy assembly package: `tls-alpn-01` shares the Panel/Naive Caddy listener on :443, HTTP-01 for a Hysteria2-only domain needs a dedicated challenge listener on TCP :80, and DNS-01 needs no listener. Conflicts on :80 for a Hysteria2-only domain are warnings, while the Panel domain remains an error.
_Avoid_: challenge listener, cert port

**Panel managed material**:
The files and systemd units that make Panel access work: `veil.env`, optional Panel TLS files, optional Panel Caddy access files, and Panel runtime units. The panel material package owns file derivation; installer and repair code are Adapters.
_Avoid_: install output bundle, repair file list

**Managed file set**:
The generic file set Module that plans missing/drifted managed file repairs and writes planned files atomically. Installer apply and repair flows use it through thin Adapters.
_Avoid_: os.WriteFile helper, repair loop

**Backup lifecycle**:
The install, repair, and rollback material safety flow that backs up managed files, stores manifests, restores originals, creates safety backups, lists backups, and cleans old backup directories.
_Avoid_: copy helper, rollback files

**Audit log**:
The append-only JSONL record of install, repair, rollback, and backup lifecycle outcomes.
_Avoid_: logging helper, event dump

**Panel state repair material**:
The desired repair actions derived from persisted Management state: Panel managed material, Generated config artifacts, and Managed systemd units needed by enabled Inbounds and WARP.
_Avoid_: repair script steps, state replay

**Inbound**:
A named proxy entry that defines protocol, transport, port, enabled state, and optional password.
_Avoid_: profile, listener, account

**Inbound catalog**:
The package Module that owns Inbound validation, duplicate Transport binding detection, credential generation, create/update/delete behavior, and safe cloning. HTTP routes and Management state mutation are Adapters over it.
_Avoid_: inbound helpers, route CRUD

**Transport binding**:
The network binding selected by an Inbound, composed from transport and port. TCP and UDP bindings may share the same numeric port because they are different transports.
_Avoid_: listener key, socket id

**Mieru**:
A proxy protocol/runtime that Veil can manage as Inbounds with TCP or UDP transport bindings. Mieru ports come from Inbounds; they are not shared proxy ports allocated by Veil install.
_Avoid_: mieru stack, mizaru

**Inbound protocol catalog**:
The protocol package source for Inbound protocol IDs, display names, allowed transports, Caddy requirement, and firewall labels, with protocol Adapters adding Generated config set rendering, Apply workflow actions, runtime metadata, and Client link delivery.
_Avoid_: protocol switch list, UI enum, scattered protocol constants

**Protocol runtime provisioning**:
The service package plan that maps enabled Inbounds and WARP state to required Managed systemd units such as `veil-mieru.service`. It is driven by Panel state, not Veil install stack selection; HTTP and repair code are Adapters.
_Avoid_: install-time protocol stack, shared runtime bundle

**Managed systemd unit**:
A systemd unit rendered, installed, repaired, controlled, observed, or removed by Veil for the Panel, protocol runtimes, or WARP. The service package owns runtime catalog behavior and manual restart control; HTTP routes and protocol capability discovery are Adapters.
_Avoid_: service component, daemon file

**Runtime command**:
A fixed external command Veil runs to validate configs, control Managed systemd units, read systemd status, or check runtime health. Runtime command execution lives outside HTTP Adapters.
_Avoid_: shell snippet, exec call

**Privileged helper**:
The root-owned, socket-activated process that accepts an allowlisted operation protocol over `/run/veil/helper.sock`, authenticates the Panel with Unix peer credentials, and performs the minimum host mutations that cannot run as the unprivileged `veil` user.
_Avoid_: root Panel, generic command runner, host shell API

**Service status**:
The Panel-facing status snapshot and log access for Managed systemd units, including runtime catalog entries, systemd load/active/sub-state fields, and bounded journald reads. The service package owns status response and log command shaping; HTTP routes are Adapters.
_Avoid_: status endpoint payload, daemon list

**Runtime observation**:
The user-facing snapshot and package for runtime facts shown by the Panel: system resources, Panel TLS, network counters, listening ports, managed processes, disk usage, and local read errors. HTTP routes are Adapters over this Module.
_Avoid_: procfs dump, stats endpoint bundle

**HTTP observability**:
The Prometheus metrics, request accounting, service status gauges, and rate-limit decisions wrapped around HTTP Adapters.
_Avoid_: metrics helpers, middleware pile

**Diagnostic tool**:
A Panel-invoked runtime utility such as DNS lookup, ping, or speedtest, implemented outside HTTP routes with route handlers acting as Adapters.
_Avoid_: utility endpoint, shell tool glue

**Client profile**:
A named user credential attached to an Inbound.
_Avoid_: profile, account, inbound

**Client link**:
A user-facing connection URI generated from Settings, enabled Inbounds, and enabled Client profiles.
_Avoid_: share link, subscription item

**Client access aggregation**:
The client access package rule that turns one or more enabled Inbounds into Client links, including protocols such as Mieru where multiple Transport bindings can produce one client config. HTTP routes are Adapters over this package.
_Avoid_: link special case, subscription merge

**Generated config set**:
The staged Caddy, Hysteria2, Mieru, WARP, and rule files derived from Settings, Inbounds, routing, and WARP state. Set assembly, cardinality, path, artifact, validation, validation pass policy, inbound renderer, and routing source material logic lives in the generated config package; HTTP and protocol capability wiring are Adapters.
_Avoid_: rendered files, output bundle

**Routing rule catalog**:
The package Module that owns Routing rule validation, lookup, preset profiles, preset application, and preset responses. HTTP routes and Management state mutation are Adapters over it.
_Avoid_: routing helpers, preset switch

**Generated config artifact**:
One file inside a Generated config set, identified by a stable generated subpath that also determines validation and live promotion paths.
_Avoid_: ad-hoc config path, output filename

**Routing source material**:
The route-dat files fetched, checksum-verified, and staged from a Routing source for WARP routing rules.
_Avoid_: downloaded routing files, rule data side effects

**Apply workflow**:
The staged-to-live package flow that validates, promotes, reloads services, checks health, rolls back when needed, and records Apply history. HTTP routes and Management state contexts are Adapters over this workflow.
_Avoid_: deploy, publish

**Apply history**:
The retained, queryable record of Apply workflow outcomes, including stage, success, generated files, live files, runtime actions, health checks, and rollback details.
_Avoid_: log list, audit dump

**Management apply intent**:
The plan-level interpretation of Management state before staging: validation errors, Generated config artifacts, Apply workflow actions, and Managed systemd units. The apply plan package owns the plan-building rules; HTTP and Management state contexts are Adapters.
_Avoid_: dry-run result, deployment intent

**Management state**:
The Settings, Inbounds, routing, WARP state, and apply history snapshot that the Panel validates and persists.
_Avoid_: config blob, raw state JSON

**WARP policy**:
The package Module that owns WARP defaults, secret preservation, and redaction before WARP state is rendered or returned.
_Avoid_: warp helpers, sing-box defaults

**Settings policy**:
The package Module that owns Settings validation, Web base path normalization, fallback root safety, and credential redaction/preservation.
_Avoid_: settings helpers, form cleanup

**Management state model**:
The shared value types for Settings, Inbounds, routing, WARP, Client links, Apply workflow responses, and snapshots that other Modules use without importing HTTP Adapters.
_Avoid_: dto pile, shared structs

**Management state mutation**:
The mutation path that validates, updates, redacts, and saves Settings, Inbounds, routing rules, and WARP inside Management state.
_Avoid_: route handler update, save wrapper

**State store**:
The persistence package for Management state, including strict codec, schema validation, default state, resource name parsing, snapshot/default handling, encryption, and decryption of secrets at rest. HTTP and Panel Modules use it through thin Adapters.
_Avoid_: database, config file wrapper

**Credential policy**:
The rules for generating, preserving, validating, redacting, encrypting, and emitting credentials for Inbounds and Client profiles.
_Avoid_: password helpers, secret utilities

**Credential disclosure**:
The rule set deciding when generated secrets are shown, redacted, stored, or emitted in client links.
_Avoid_: logging, masking

## Relationships

- A **Veil install** always produces **Panel access** and credentials; its orchestration Module owns prompt, requirement, preview, prerequisite, confirmation, and apply order; it does not select or install protocol stacks.
- **Host environment** owns platform, domain/email, public IP, and DNS checks outside the installer orchestration Module.
- **Firewall material** owns firewall rule planning, firewall status probing, and Panel-facing firewall rule response shaping outside HTTP Adapters.
- **Veil update** owns release catalog, asset verification, archive extraction, binary replacement, systemd restart, Panel health checking, and rollback material outside the Cobra command Adapter.
- **Veil version** owns latest-release fetching, version comparison, and update guidance outside the Cobra command Adapter.
- **Veil serve** owns listen/auth/path/TLS/Web base path resolution outside the Cobra command Adapter.
- **Veil status** owns Panel status fetch, generated local Panel TLS trust, and human/JSON rendering outside the Cobra command Adapter.
- **Veil doctor** owns command readiness checks and human/JSON readiness rendering outside the Cobra command Adapter.
- **Veil uninstall** owns preview, service stop/disable actions, binary removal, `--keep-data`/`--purge` handling, and daemon reload outside the Cobra command Adapter; default removal clears `/etc/veil` and `/var/lib/veil`, while `--keep-data` preserves them.
- **Veil rollback** owns backup listing, restore, cleanup, and rollback audit event ordering outside the Cobra command Adapter.
- **Veil repair** owns profile selection, repair plan preview, dry-run, and apply ordering outside the Cobra command Adapter.
- A **Panel URL** contains exactly one **Web base path**.
- **Panel access** may be direct/local without a **Panel URL**, or **Panel Caddy access** with a **Panel URL**; the Panel access package owns the decision-to-material rules.
- Direct/local **Panel access** uses **Panel TLS** by default; **Panel Caddy access** uses Caddy TLS instead.
- `direct` **Panel access** may replace **Panel TLS** with a trusted **Panel IP certificate** when the IP certificate issuance succeeds.
- A **Hysteria2 Inbound** assigned a domain uses **Hysteria2 Inbound TLS**: it reuses the Caddy-managed certificate for a Panel/NaiveProxy domain, or obtains its own via an **ACME challenge binding** on TCP :80 for a Hysteria2-only domain.
- **Panel managed material** is derived from **Panel access** in the panel material package and is applied through a **Managed file set** by both Veil install and repair.
- **Backup lifecycle** protects managed file changes during install, repair, apply, and rollback.
- **Audit log** records install, repair, rollback, and backup lifecycle outcomes outside CLI Adapters.
- **Panel state repair material** extends persisted **Management state** into repair actions for generated files and runtime units.
- The **Panel** is assembled from **Panel slices** and apply workflow command bindings through the Panel rendering package; HTTP routes are thin Adapters over rendered HTML.
- The **Panel** manages zero or more **Inbounds**.
- The **Inbound catalog** owns Inbound validation, credential completion, duplicate **Transport binding** checks, and cloning outside HTTP Adapters.
- Each **Inbound** has exactly one **Transport binding**.
- **Transport bindings** are selected per **Inbound** and are independent of Veil install shared proxy port planning.
- Each **Inbound** can contain zero or more **Client profiles**.
- Each enabled **Client profile** can produce one **Client link** when its **Inbound** is enabled and allowed by Settings; **Client access aggregation** may combine multiple Transport bindings for a protocol outside HTTP Adapters.
- **Management apply intent** is derived from **Management state** by the apply plan package before the **Apply workflow** writes staged files.
- **Apply history** records retained **Apply workflow** outcomes, retention, filtering, and persistence outside HTTP Adapters.
- **Apply workflow** orchestration lives outside HTTP routes; Management contexts adapt it to staged files and runtime commands.
- A **Generated config set** contains **Generated config artifacts** and **Routing source material** from the generated config package that are promoted by the **Apply workflow**.
- The **Routing rule catalog** owns routing rule validation, preset profiles, preset application, and preset response shaping outside HTTP Adapters.
- **Protocol runtime provisioning** is derived from enabled **Inbounds** and WARP state and selects **Managed systemd units**.
- Non-privileged **Runtime commands** are executed through an Adapter; live promotion, service control, journald reads, backup/key operations, verified updates, and restart cross the **Privileged helper** boundary.
- The Managed systemd unit catalog maps promoted Generated config artifacts to service reload/restart actions, action success policy, health collection, health policy, and **Service status** outside HTTP Adapters.
- **Runtime observation** concentrates procfs parser output into the Panel-facing observation model outside HTTP Adapters.
- **HTTP observability** concentrates metrics exposition, request accounting, status recording, and rate-limit decisions outside HTTP Adapters.
- **Diagnostic tools** concentrate DNS lookup, ping, and speedtest behavior outside HTTP Adapters.
- **Management state model** provides the import-safe value types used by Panel, CLI, protocols, generated configs, and persistence Modules.
- **Management state mutation** changes **Management state** before the **State store** persists it.
- **Settings policy** owns Settings validation, normalization, and redaction outside HTTP Adapters.
- **WARP policy** owns WARP defaults and secret redaction outside HTTP Adapters.
- The **State store** persists and validates **Management state** outside HTTP Adapters.
- **Credential policy** governs generated and preserved Inbound and Client profile credentials.
- **Credential disclosure** governs install summaries, previews, client links, and state persistence.
- `stack` is a removed Settings JSON field, not a current Settings **Interface** field; protocol choices belong to Panel **Inbounds** and unknown `stack` input is rejected at strict JSON **Interfaces**.

## Example dialogue

> **Dev:** "When the user creates a new **Inbound** without a password, should the **Client link** use the global password?"
> **Domain expert:** "No — new Inbounds should get their own generated password. Empty password during update means preserve the existing one."
>
> **Dev:** "Should the **Panel URL** include the random panel port?"
> **Domain expert:** "No — the **Panel** is served through Caddy on HTTPS, so the **Panel URL** is `https://domain/WebBasePath/`."

## Flagged ambiguities

- "profile" was used for install presets, proxy entries, and user credentials — resolved: use **Veil install** for setup presets, **Inbound** for proxy entries, and **Client profile** for user credentials.
- "webBasePath" appears as Go field naming; in prose use **Web base path**.
- "service" may mean systemd unit or domain module — in architecture reviews use **Module** for code structure and name systemd units explicitly.
- Multiple enabled **Inbounds** of the same protocol can produce multiple **Client links**. Hysteria2, olcRTC, and NaiveProxy are rendered as isolated per-Inbound Generated config artifacts and managed through systemd template units. Mieru **Inbounds** aggregate into one **Generated config set** so TCP and UDP **Transport bindings** can share a numeric port.
- Protocol-specific metadata should live in the **Inbound protocol catalog** package, and protocol-specific behavior should live behind **Inbound protocol catalog** Adapters so adding another protocol does not require editing Panel copy, Inbound form options, firewall planning, Generated config set rendering, Client link delivery, and Apply workflow logic separately.
- `both` is not a valid product concept now that Veil has more than two protocols. Do not introduce new user-facing stack choices.
