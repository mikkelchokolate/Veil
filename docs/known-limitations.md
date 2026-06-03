# Veil Known Limitations

This document lists design constraints, architectural boundaries, and known limitations in the current version of Veil.

---

## 1. Multiple Inbounds of the Same Protocol

### Overview
Veil supports multiple Inbounds, but there is an architectural limitation regarding how certain proxy protocol runtimes generate their configuration files:

- **NaiveProxy & Hysteria2:** Currently, if you configure multiple enabled Inbounds of the same protocol (e.g., two separate Hysteria2 Inbounds), they cannot be merged into a single configuration file. Applying such a configuration will result in an validation error or staging failure rather than silently overwriting the generated config.
- **Mieru:** Mieru Inbounds support aggregation. If you define multiple Mieru Inbounds, their transport bindings and client profiles are aggregated cleanly into a single generated configuration file, allowing TCP and UDP configurations to coexist.

### Workaround
- If you need multiple users on Hysteria2 or NaiveProxy, add multiple **Client Profiles** under the same Inbound instead of creating separate Inbound instances.
- If you must run separate instances on different ports, you will need to manage the additional daemon instances manually outside of Veil's automated systemd control plane.

---

## 2. Platform and systemd Dependency

- **Operating System:** Veil's control plane is designed for **Linux** with **systemd**. While you can compile the `veil` binary and run the HTTP Panel on Windows or macOS (for testing or local administration), the background daemon cannot automatically reload firewall rules or control proxy runtimes on non-Linux platforms.
- **Environment:** Runtimes must be installed on the local system (bare-metal) or the container must have read-write access to the host's systemd socket (`/run/systemd/system`) to allow service orchestration.

---

## 3. Go Module Path Mismatch

- **Path:** The module is named `github.com/veil-panel/veil`, but the active repository is hosted at `github.com/mikkelchokolate/Veil`.
- **Implication:** Running `go install github.com/mikkelchokolate/Veil/cmd/veil@latest` directly will fail. To compile from source, you must clone the repository first and run the build within the cloned workspace folder.
