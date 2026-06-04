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

### 4. Current UI/UX Capabilities
- Client configuration export flows are available from the Panel, including
  JSON/subscription downloads, copyable links, and local QR rendering that does
  not send client URIs to third-party services.
- The Users screen supports admin/viewer accounts, active session audit and
  revocation, and browser-side replacement API token generation with cutover
  guidance.
- Viewer sessions are read-only in the UI and API: status, diagnostics, logs,
  client exports, and generated previews remain inspectable, while save,
  delete, restart, apply, and user/session management actions require admin.
- Apply flows include a safe file-level preview for generated configs,
  promoted files, backups, rollback files, runtime actions, and DNS/TLS/
  firewall/service-impact warnings before operators confirm changes.

### 5. Backup & DR Orchestration
- Add scheduled backup retention policies around the shipped `veil backup create / restore` commands.
- Add operator-facing restore drills and compatibility fixtures for long-lived production archives.

---

## Long-Term Objectives

### 6. UI/UX Evolution
- Internationalization (i18n) support.
- Interactive setup wizard for first-time deployments.
- Rich visual configuration previews before apply, while continuing to avoid
  secret-bearing generated config disclosure in the browser.
- Real-time UI/API port collision warnings prior to staging config updates.
- Clearer protocol-specific remediation text for DNS, TLS, firewall, and
  runtime health failures.
