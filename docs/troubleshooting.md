# Veil Troubleshooting Guide

This guide provides steps to diagnose, debug, and recover your Veil installation in case of failure.

---

## 1. Verifying Status and Health

`veil help` lists every CLI command. Veil also provides built-in tools to inspect the daemon, services, and system configuration:

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

### Protocol fails with status 203/EXEC ("Failed to locate executable")

A protocol unit that fails to start with `status=203/EXEC` means its runtime
binary is not present at the path the systemd unit invokes (for example
`/usr/local/bin/mita` for Mieru, `/usr/local/bin/hysteria` for Hysteria2).
`veil install` provisions these runtimes automatically, but if a download was
skipped or failed you can (re)install them at any time:

```bash
sudo veil runtime install
# or a single protocol:
sudo veil runtime install --only mieru
```

This downloads `caddy`, `hysteria`, `mita`, and `sing-box` from their upstream
GitHub releases (verifying published checksums) and builds `olcrtc` from source
with the Go toolchain. Re-run `veil doctor` afterward to confirm the binaries
are present, then restart the affected unit.

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

### Hysteria2 Connects With `insecure` Only (Self-Signed Certificate)
- **Symptom:** A Hysteria2 Inbound assigned a domain works only when the client sets `insecure: true`; the served certificate is self-signed instead of a trusted Let's Encrypt one.
- **Cause:** The domain is Hysteria2-only (not the Panel's or a NaiveProxy Inbound's domain), so its certificate uses the HTTP-01 challenge, and TCP `:80` was unavailable at apply time (or the domain did not resolve to the host). Veil logged a warning and kept the Inbound on a self-signed certificate.
- **Resolution:** Confirm the domain's DNS points at this host and that TCP `:80` is free (`ss -tulpn | grep ':80 '`) or already served by Caddy, then re-apply. Reusing the Panel or a NaiveProxy Inbound's domain avoids `:80` entirely (shared `tls-alpn-01`). Check the challenge listener and issuance in the Caddy journal: `journalctl -u veil-caddy.service -n 100 --no-pager`.

### Direct Panel Shows a Certificate Warning by IP
- **Symptom:** In `direct` mode the browser warns about the certificate when opening `https://<server-ip>:<port>/`.
- **Cause:** The trusted Let's Encrypt IP certificate was not issued — typically because port `80/tcp` was not reachable during the HTTP-01 challenge — so Veil fell back to the self-signed Panel certificate.
- **Resolution:** Ensure port `80/tcp` is free and reachable from the internet, then re-issue via `veil repair --le-ip-cert --yes` (or reinstall). Verify the served certificate's SAN is the public IP (`openssl s_client -connect <ip>:<port> | openssl x509 -noout -ext subjectAltName`).

### "Apply Live" Failures on systemd Runtimes
- **Symptom:** Staging works, but applying changes fails on systemd operations.
- **Cause:** The non-root Panel could not reach the bare-metal privileged helper, the helper rejected a path/unit outside its allowlist, or the target runtime failed.
- **Resolution:** Check `veil-helper.socket` and `veil-helper.service`, then inspect the helper journal and the target runtime journal. Rootless containers support local/read-only administration and staging, not live host systemd orchestration.

### Privileged Helper Is Unavailable
- **Symptom:** A privileged API returns JSON matching `{"error":{"code":"operation_failed","message":"..."}}`, or Panel actions report that `/run/veil/helper.sock` is unavailable.
- **Cause:** Socket activation is disabled, permissions do not allow the `veil` account to connect, or the helper failed its policy checks.
- **Resolution:**

```bash
systemctl status veil-helper.socket veil-helper.service
journalctl -u veil-helper.service -n 100 --no-pager
stat -c '%U:%G %a %n' /run/veil/helper.sock
systemctl restart veil-helper.socket
```

The expected socket mode is `root:veil 0660`. Do not replace it with a
world-writable socket or give the Panel process root privileges.
