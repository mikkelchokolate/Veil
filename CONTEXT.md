# Veil Context

Veil is a control plane for installing, configuring, and operating NaiveProxy and Hysteria2 through a panel and CLI. This context records the domain language used by the codebase and architecture reviews.

## Language

**Veil install**:
An interactive or scripted setup flow that chooses ports, generates credentials, renders managed files, and prints the panel access summary.
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

**Inbound**:
A named proxy entry that defines protocol, transport, port, enabled state, and optional password.
_Avoid_: profile, listener, account

**Client link**:
A user-facing connection URI generated from Settings and enabled Inbounds.
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

- A **Veil install** produces a **Panel URL**, credentials, and a **Generated config set**.
- A **Panel URL** contains exactly one **Web base path**.
- The **Panel** manages zero or more **Inbounds**.
- Each **Inbound** can produce one **Client link** when enabled and allowed by Settings.
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

- "profile" was used for both install profiles and proxy entries — resolved: use **Veil install** for setup presets and **Inbound** for proxy entries.
- "webBasePath" appears as Go field naming; in prose use **Web base path**.
- "service" may mean systemd unit or domain module — in architecture reviews use **Module** for code structure and name systemd units explicitly.
