# Durable Sessions And Audit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make browser sessions survive restarts without storing bearer secrets,
revoke sessions when user authority changes, and record security-sensitive panel
activity in a rotated, redacted audit log.

**Architecture:** Replace the process-global raw-token map with an atomic JSON
session store keyed by SHA-256 token hashes. Attach one registry and one audit
recorder to each management state so test and production routers have explicit
ownership. Keep raw session and CSRF values only in browser responses and
requests.

**Tech Stack:** Go 1.25, `net/http`, SHA-256, atomic file replacement, JSONL,
existing management API and test helpers.

---

### Task 1: Persist Hashed Sessions

- [ ] Add failing unit tests for restart survival, secret-free files, idle and
  absolute expiry, user revocation, current-session preservation, and file
  permissions.
- [ ] Implement a concurrency-safe registry with atomic `0600` persistence,
  30-minute idle expiry, 24-hour absolute expiry, and hashed token/CSRF values.
- [ ] Move login, logout, status, middleware, panel rendering, and session
  administration from `globalSessions` to the management state's registry.
- [ ] Revoke a user's sessions after password or role changes and deletion.
- [ ] Add an HTTP restart test proving that an existing cookie remains valid
  after constructing a new router from the same state directory.
- [ ] Run `go test ./internal/api -run "Session|Auth|User" -count=1`.
- [ ] Commit as `feat: persist and harden panel sessions`.

### Task 2: Add Rotated Structured Audit

- [ ] Add failing tests for JSONL append, rotation, redaction, and newest-first
  querying.
- [ ] Implement an audit recorder with bounded files, retained generations, and
  recursive secret-key redaction.
- [ ] Record setup, login success/failure, logout, session revocation, user
  changes, configuration mutations, apply, and service actions.
- [ ] Add an admin-only `GET /api/audit` endpoint with bounded pagination.
- [ ] Add HTTP tests for authorization and auth event recording.
- [ ] Run `go test ./internal/audit ./internal/api -run "Audit|Login|Setup|User" -count=1`.
- [ ] Commit as `feat: add structured panel audit history`.

### Task 3: Document And Verify

- [ ] Describe session lifetime, restart behavior, revocation, audit location,
  rotation, redaction, and API schemas in operator docs and OpenAPI.
- [ ] Run `gofmt`, `go test ./...`, OpenAPI lint, and `git diff --check`.
- [ ] Commit as `docs: document durable sessions and audit`.
