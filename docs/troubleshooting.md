# Veil Troubleshooting Guide

This guide provides steps to diagnose, debug, and recover your Veil installation in case of failure.

---

## 1. Verifying Status and Health

Veil provides built-in tools to inspect the daemon, services, and system configuration directly from the CLI.

### Inspect Service Status
Use `veil status` to retrieve a list of managed systemd units (panel, naiveproxy, hysteria2, olcrtc, mieru, warp) and their active states:

```bash
# Print status in a human-readable format
veil status

# Return status in JSON format
veil status --json
```

### Checking System Environment
Use `veil doctor` to verify that all necessary system dependencies, helper binaries, and network configuration pathways are available:

```bash
veil doctor
```
If some binaries (like `caddy`, `hysteria`, or `mieru`) are missing, `veil doctor` will output them as warnings.

---

## 2. Check Log Locations

When investigating failures (e.g., a protocol failing to start or settings failing to apply), inspect the logs.

### Systemd Journal Logs
Since Veil runtimes run as systemd services, their standard output is captured by journald:

```bash
# Core panel daemon logs
journalctl -u veil.service -n 100 --no-pager

# Specific protocol runtime logs
journalctl -u veil-caddy@<inbound>.service -n 100 --no-pager
journalctl -u veil-hysteria2@<inbound>.service -n 100 --no-pager
journalctl -u veil-mieru.service -n 100 --no-pager
```

### Audit Log
All management and lifecycle events (install, uninstall, repair, rollback, backup) are appended to the JSONL format audit log:

```bash
cat /var/log/veil/audit.jsonl
```
Each line corresponds to an operation, containing a timestamp, action name, user parameters, and execution outcome.

---

## 3. Backups and Configuration Rollbacks

If a change breaks your proxy connections or locks you out of the Panel, you can revert to a previous working configuration state.

### List Available Backups
Every successful update or repair writes a complete state snapshot backup. To list them:

```bash
veil rollback list
```
Each backup lists a unique backup ID, timestamp, and active configuration summary.

### Restoring a Backup
Revert your active state to a specific backup ID:

```bash
veil rollback restore <backup-id> --yes
```
This command restores the state file, stages the generated files, restarts the associated units, and records the event in the audit trail.

---

## 4. Common Errors and Resolutions

### Port Collision (Address Already In Use)
- **Symptom:** A protocol service (e.g. `veil-mieru`) fails to start, and logs show `bind: address already in use`.
- **Cause:** Another process on the host is already bound to that transport/port combination.
- **Resolution:** Identify the colliding process using `ss -tulpn` or change the Inbound port in the Panel. Note that TCP and UDP bindings on the same numeric port do not conflict.

### Panel Access Forbidden (401 Unauthorized)
- **Symptom:** `/api/*`, `/healthz` on a public listener, or `/metrics` with authenticated metrics returns `401 Unauthorized`.
- **Cause:** The request has no valid `VEIL_API_TOKEN`, bearer token, or `veil_session` cookie. Cookie-backed mutating requests may also be missing `X-CSRF-Token`.
- **Resolution:** For API clients, view the correct token in `/etc/veil/veil.env` under `VEIL_API_TOKEN` and attach `Authorization: Bearer <token>` or `X-Veil-Token: <token>`. For browser access, sign in through the Panel login page. If no user exists, run `sudo veil admin reset` or `sudo veil admin set --username admin --password '...' --role admin`.

### Public Serve Refuses to Start
- **Symptom:** `veil serve --listen 0.0.0.0:2096` exits before binding.
- **Cause:** Public listeners are fail-closed and require both token auth and user/session auth.
- **Resolution:** Set `VEIL_API_TOKEN` or pass `--auth-token`, then initialize a Panel admin user with `veil admin reset` or `veil admin set`.

### "Apply Live" Failures on systemd Runtimes
- **Symptom:** Staging works, but applying changes fails on systemd operations.
- **Cause:** Veil expects to interact with the host systemd daemon. If running inside a standard Docker container without mounting systemd sockets, systemd reloads will fail.
- **Resolution:** If deploying via Docker, ensure you run under loopback/local mode, or configure `/host-systemd` mounts as shown in the hardening and Docker guides.
