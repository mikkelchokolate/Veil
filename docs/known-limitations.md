# Veil Known Limitations

This document lists design constraints, architectural boundaries, and known limitations in the current version of Veil.

---

## 1. Multiple Inbounds of the Same Protocol

### Overview
Veil supports multiple Inbounds, with varying levels of isolation depending on the proxy protocol:

- **Hysteria2 & olcRTC:** Full isolation. Defining multiple enabled Inbounds for Hysteria2 or olcRTC generates isolated configuration files and spawns independent daemon processes managed via systemd template units (`veil-hysteria2@<inbound>.service` and `veil-olcrtc@<inbound>.service`).
- **Mieru:** Aggregation. If you define multiple Mieru Inbounds, their port/transport bindings and client profiles are aggregated cleanly into a single generated configuration file managed by a single daemon process.
- **NaiveProxy:** Full isolation. Defining multiple enabled Inbounds for NaiveProxy generates isolated configuration files (`<inbound>.Caddyfile`) and spawns independent Caddy processes managed via systemd template units (`veil-caddy@<inbound>.service`).

- If you need multiple users on Hysteria2 or NaiveProxy, add multiple **Client Profiles** under the same Inbound instead of creating separate Inbound instances.

---

## 2. Platform and systemd Dependency

- **Operating System:** Veil's control plane is designed for **Linux** with **systemd**. While you can compile the `veil` binary and run the HTTP Panel on Windows or macOS (for testing or local administration), the background daemon cannot automatically reload firewall rules or control proxy runtimes on non-Linux platforms.
- **Environment:** Full live service orchestration requires runtimes installed on the Linux host and the bare-metal privileged helper. A rootless container can provide loopback/read-only administration and staging, but mounting the host systemd tree or socket into the container is not a supported substitute for the helper policy boundary.

---

## 3. Certificate Issuance Boundaries

- **Hysteria2-only domains need TCP :80.** A Hysteria2 Inbound using a domain not shared with the Panel or NaiveProxy requires the HTTP-01 challenge, which binds a dedicated challenge listener on TCP `:80`. If `:80` is unavailable (and not already served by Caddy), the apply succeeds with a warning but the certificate is not issued; the Inbound serves a self-signed certificate until `:80` is freed and the Inbound re-applied. Reusing the Panel's or a NaiveProxy Inbound's domain avoids this requirement (shared `tls-alpn-01`).
- **Let's Encrypt IP certificates are short-lived and IPv4/IPv6-address-only.** The `direct`-mode IP certificate uses the `shortlived` profile (3-day validity) with the SAN set to the public IP address and no `CN`. Some clients or browsers do not accept IP-address SANs the same way as DNS SANs. Issuance requires port `80/tcp` reachable from the internet during the HTTP-01 challenge.
- **Certificate issuance happens in install/repair, not in a routine apply.** Switching Panel access to `direct` through the API updates state, but the IP certificate is requested by `veil install` / `veil repair`; a normal `/api/apply` does not request the IP certificate on its own.

---

## 4. Go Module Path

- **Path:** The module path is canonicalized to `github.com/mikkelchokolate/Veil`, matching the GitHub repository URL.
- **Implication:** You can install the CLI tool directly from GitHub via `go install github.com/mikkelchokolate/Veil/cmd/veil@latest`.
