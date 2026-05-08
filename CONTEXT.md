# Veil Context

Veil is a control plane for installing, configuring, and operating NaiveProxy, Hysteria2, and Mieru through a panel and CLI. This context records the domain language used by the codebase and architecture reviews.

## Language

**Veil install**:
An interactive or scripted setup flow that chooses Panel access, optionally chooses proxy runtimes, generates credentials, renders managed files, and prints the panel access summary.
_Avoid_: bootstrap, setup wizard

**Panel**:
The browser UI and HTTP management surface used to operate Veil after install.
_Avoid_: dashboard, admin site

**Web base path**:
A random URL path prefix that hides the Panel behind Caddy and routes browser requests to Veil.
_Avoid_: secret route, slug

**Panel URL**:
The final HTTPS URL a user opens to reach the Panel, composed from domain and Web base path.
_Avoid_: dashboard link, admin URL

**Panel access**:
The way a user reaches the Panel: direct HTTP listen address, local-only listen address for SSH tunneling, or HTTPS through Caddy.
_Avoid_: dashboard exposure, admin binding

**Inbound**:
A named proxy entry that defines protocol, transport, port, enabled state, and optional password.
_Avoid_: profile, listener, account

**Transport binding**:
The network binding selected by an Inbound, composed from transport and port. TCP and UDP bindings may share the same numeric port because they are different transports.
_Avoid_: listener key, socket id

**Mieru**:
A proxy protocol/runtime that Veil can manage as Inbounds with TCP or UDP transport bindings. Mieru ports come from Inbounds; they are not shared proxy ports allocated by Veil install.
_Avoid_: mieru stack, mizaru

**Inbound protocol catalog**:
The protocol capability source for Inbound protocol IDs, display names, allowed transports, stack inclusion, Caddy requirement, firewall labels, Generated config set rendering, Apply workflow actions, and Client link delivery.
_Avoid_: protocol switch list, UI enum, scattered protocol constants

**Client profile**:
A named user credential attached to an Inbound.
_Avoid_: profile, account, inbound

**Client link**:
A user-facing connection URI generated from Settings, enabled Inbounds, and enabled Client profiles.
_Avoid_: share link, subscription item

**Generated config set**:
The staged Caddy, Hysteria2, WARP, and rule files derived from Settings, Inbounds, routing, and WARP state.
_Avoid_: rendered files, output bundle

**Apply workflow**:
The staged-to-live flow that validates, promotes, reloads services, checks health, and rolls back when needed.
_Avoid_: deploy, publish

**State store**:
The persistence module for management state, including encryption and decryption of secrets at rest.
_Avoid_: database, config file wrapper

**Credential disclosure**:
The rule set deciding when generated secrets are shown, redacted, stored, or emitted in client links.
_Avoid_: logging, masking

## Relationships

- A **Veil install** always produces **Panel access** and credentials, and may produce a **Generated config set** when proxy runtimes are selected.
- A **Panel URL** contains exactly one **Web base path**.
- **Panel access** may be direct/local without a **Panel URL**, or HTTPS through Caddy with a **Panel URL**.
- The **Panel** manages zero or more **Inbounds**.
- Each **Inbound** has exactly one **Transport binding**.
- **Transport bindings** are selected per **Inbound** and are independent of Veil install shared proxy port planning.
- Each **Inbound** can contain zero or more **Client profiles**.
- Each enabled **Client profile** can produce one **Client link** when its **Inbound** is enabled and allowed by Settings.
- The **Generated config set** is promoted by the **Apply workflow**.
- The **State store** persists Settings, Inbounds, routing, WARP state, and apply history.
- **Credential disclosure** governs install summaries, previews, client links, and state persistence.

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
- Multiple enabled **Inbounds** of the same protocol can produce multiple **Client links**, but NaiveProxy and Hysteria2 are not yet renderable into one **Generated config set**; apply plan must reject those instead of silently overwriting generated files.
- Mieru **Inbounds** are expected to aggregate into one **Generated config set** so TCP and UDP **Transport bindings** can share a numeric port.
- Protocol-specific behavior should live behind catalog **Modules** so adding another protocol does not require editing Panel copy, Inbound form options, firewall planning, Generated config set rendering, Client link delivery, and Apply workflow logic separately.
