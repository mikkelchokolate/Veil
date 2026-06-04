# Veil Project Roadmap

This document outlines key technical milestones, feature proposals, and architectural improvements planned for Veil.

---

## Short-Term Milestones

### 1. Hardening & Safety Controls
- **TLS Warnings**: Enforce strong warnings or refusal-to-start when listening publicly without active TLS.
- **Access Logs**: Complete structured log rotation and audit logging for session authentication gates.

### 2. Multi-Inbound support for NaiveProxy
- Support Caddy multi-instance mapping (`veil-caddy@<inbound>.service`) to mirror Hysteria2 and olcRTC template isolation.
- Add real-time UI/API port collision and conflict warnings prior to staging config updates.

---

## Medium-Term Goals

### 3. Expanded Protocol Integrations
- Integrate additional transport layers and runtime targets.
- Standardize multi-user profiles across all supported protocols (matching Hysteria2 and Mieru capability).

### 4. Backup & DR Orchestration
- Add scheduled backup retention policies around the shipped `veil backup create / restore` commands.
- Add operator-facing restore drills and compatibility fixtures for long-lived production archives.

---

## Long-Term Objectives

### 5. UI/UX Evolution
- Internationalization (i18n) support.
- Interactive setup wizard for first-time deployments.
- Client configuration export flows, including QR codes and copyable links for
  supported protocol profiles.
- Token rotation from the Panel UI, with clear cutover guidance for API clients
  and automation.
- User/session management screens for creating, disabling, and auditing Panel
  users and active sessions.
- A read-only viewer role in the UI so operators can inspect status,
  diagnostics, logs, and generated previews without mutation permissions.
- Safe apply preview with a file-level diff of generated config, unit, firewall,
  DNS, and TLS changes before the operator confirms the apply.
- Clear pre-apply warnings for DNS, TLS, firewall, and port-collision risks.
