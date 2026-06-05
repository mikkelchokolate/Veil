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

## 3. Go Module Path

- **Path:** The module path is canonicalized to `github.com/mikkelchokolate/Veil`, matching the GitHub repository URL.
- **Implication:** You can install the CLI tool directly from GitHub via `go install github.com/mikkelchokolate/Veil/cmd/veil@latest`.
