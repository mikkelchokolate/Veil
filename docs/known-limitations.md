# Veil Known Limitations

This document lists design constraints, architectural boundaries, and known limitations in the current version of Veil.

---

## 1. Multiple Inbounds of the Same Protocol

### Overview
Veil supports multiple Inbounds, with varying levels of isolation depending on the proxy protocol:

- **Hysteria2 & olcRTC:** Full isolation. Defining multiple enabled Inbounds for Hysteria2 or olcRTC generates isolated configuration files and spawns independent daemon processes managed via systemd template units (`veil-hysteria2@<inbound>.service` and `veil-olcrtc@<inbound>.service`).
- **Mieru:** Aggregation. If you define multiple Mieru Inbounds, their port/transport bindings and client profiles are aggregated cleanly into a single generated configuration file managed by a single daemon process.
- **NaiveProxy:** Shared Caddy process. All enabled NaiveProxy Inbounds are rendered into one consolidated Caddy configuration (`caddy/config.json`) served by a single `veil-caddy.service` unit — there are no per-Inbound Caddy processes or `veil-caddy@` template units.

- If you need multiple users on Hysteria2 or NaiveProxy, add multiple **Client Profiles** under the same Inbound instead of creating separate Inbound instances.

---

## 2. Platform and systemd Dependency

- **Operating System:** Veil's control plane is designed for **Linux** with **systemd**. While you can compile the `veil` binary and run the HTTP Panel on Windows or macOS (for testing or local administration), the background daemon cannot automatically reload firewall rules or control proxy runtimes on non-Linux platforms.
- **Protocol daemons cannot use a private or loopback DNS resolver.** The Hysteria2, Mieru, WARP and olcRTC units run under a kernel egress filter (`IPAddressDeny=` every private, loopback, link-local and CGNAT range) that only pierces the systemd-resolved stubs `127.0.0.53`/`127.0.0.54`. Hysteria2 and the WARP sing-box bridge keep the host resolver when `/etc/resolv.conf` names a reachable nameserver (the resolved stub or a public IP). When none is reachable (AWS VPC `172.31.0.2`, `169.254.169.253`/`.254` on AWS/GCP/OCI, Alibaba `100.100.2.136`, LXC/home-lab `10.x`/`192.168.x`, a local dnsmasq/unbound on `127.0.0.1`), the rendered config pins Cloudflare DNS-over-HTTPS instead: `1.1.1.1:443`, or `[2606:4700:4700::1111]:443` on hosts that route only IPv6 to Cloudflare. This choice is made at render time, so re-apply after changing the host's DNS setup. The WARP sing-box bridge follows the same rule: on such hosts its rendered config gets a `dns` section with the same Cloudflare DoH server, dialed directly rather than through the tunnel, set as `route.default_domain_resolver`, so the WireGuard peer, client domains and rule-set downloads resolve. olcRTC always uses `1.1.1.1:53`. Mieru (mita) has no resolver setting, so on such hosts `veil-mieru` runs `veil egress-resolv-conf` as root at every start and bind-mounts the resulting `/run/veil-mieru/resolv.conf` (Cloudflare `1.1.1.1`/`1.0.0.1`, or `2606:4700:4700::1111`/`::1001` on IPv6-only hosts) over `/etc/resolv.conf` in its own mount namespace. That is plain DNS on port 53, because mita's stub resolver cannot speak DoH; enable systemd-resolved or configure a reachable public nameserver to keep the host resolver. On a host with no `/etc/resolv.conf` at all Mieru gets no fallback and its domain lookups fail (#1288). `veil doctor`, root `veil install` (the `egress-dns` row of the capability report) and `veil repair` report whether this host's nameservers are reachable under the filter and warn when they are not.
- **Environment:** Full live service orchestration requires runtimes installed on the Linux host and the bare-metal privileged helper. A rootless container can provide loopback/read-only administration and staging, but mounting the host systemd tree or socket into the container is not a supported substitute for the helper policy boundary.
- **First-use runtime builds need internet access and have a 15-minute apply budget.** Caddy and olcRTC may need source builds; completion within that budget has not been measured on a 1-vCPU/1-GB host. A timeout leaves a retryable failed apply instead of promoting an incomplete runtime set. On a constrained host, preinstall the required runtime with `sudo veil runtime install --only naiveproxy` (or `olcrtc`) from the recovery menu or terminal, then retry the apply. In-memory API embeddings without durable state require preinstalled runtimes and do not download while holding the management lock.

---

## 3. Certificate Issuance Boundaries

- **Hysteria2-only domains need TCP :80.** A Hysteria2 Inbound using a domain not shared with the Panel or NaiveProxy requires the HTTP-01 challenge, which binds a dedicated challenge listener on TCP `:80`. If `:80` is unavailable (and not already served by Caddy), the apply succeeds with a warning but the certificate is not issued; the Inbound serves a self-signed certificate until `:80` is freed and the Inbound re-applied. Reusing the Panel's or a NaiveProxy Inbound's domain avoids this requirement (shared `tls-alpn-01`).
- **Let's Encrypt IP certificates are short-lived and IPv4/IPv6-address-only.** The `direct`-mode IP certificate uses the `shortlived` profile (3-day validity) with the SAN set to the public IP address and no `CN`. Some clients or browsers do not accept IP-address SANs the same way as DNS SANs. Issuance requires port `80/tcp` reachable from the internet during the HTTP-01 challenge.
- **Caddy-fronted IP-certificate renewal needs a post-upgrade Caddy re-render.** Hosts installed before the Caddy-fronted HTTP-01 path run a `veil-caddy.service` configuration without the `/.well-known/acme-challenge/` passthrough to the internal issuer port (`44080`). Renewal parks on `44080` whenever Caddy owns `:80`, but the route only exists after a management apply (or reinstall/repair) re-renders and reloads Caddy — run one apply after upgrading before relying on `veil cert renew` or the renewal worker.
- **Certificate issuance happens in install/repair, not in a routine apply.** Switching Panel access to `direct` through the API updates state, but the IP certificate is requested by `veil install` / `veil repair`; a normal `/api/apply` does not request the IP certificate on its own.

---

## 4. State Schema and Protocol Plugins

- **Management state is versioned.** State persists at a specific schema version (currently v4); Veil migrates older schemas forward on load (for example, legacy flat protocol fields move into `protocolFields`) but **rejects a state schema newer than the running release**. Always match a restored `state.json` to a Veil release that supports that schema, and validate it with `veil config validate` before starting the Panel.
- **Protocols are plugins.** NaiveProxy, Hysteria2, Mieru, and olcRTC are implemented as plugins under `internal/protocols/<protocol>` (config renderer, runtime descriptor, validator, UI field schema, client-link builder). Adding a protocol means adding a plugin; the Panel, firewall, generated-config, and apply wiring consume the central registry rather than per-protocol conditionals.

---

## 5. Go Module Path

- **Path:** The module path is canonicalized to `github.com/mikkelchokolate/Veil`, matching the GitHub repository URL.
- **Implication:** You can install the CLI tool directly from GitHub via `go install github.com/mikkelchokolate/Veil/cmd/veil@latest`.
