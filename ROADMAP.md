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
- Build CLI support for automated encrypted state backups (`veil backup create / restore`).
- Introduce automated rotation of the AES encryption key store (`/etc/veil/state.key`).

---

## Long-Term Objectives

### 5. UI/UX Evolution
- Internationalization (i18n) support.
- Interactive setup wizard for first-time deployments.
- Visual configuration previews highlighting files changed before applying updates.
