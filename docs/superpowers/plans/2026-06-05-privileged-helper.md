# Privileged Helper Separation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run the network-facing Panel as an unprivileged `veil` user and route every privileged Panel operation through a root helper with a typed, allowlisted Unix-socket protocol.

**Architecture:** `internal/privileged` defines a narrow client interface, request protocol, policy, local adapter, and Linux Unix-socket server. The helper verifies `SO_PEERCRED`, accepts only the configured `veil` UID, resolves all paths beneath managed roots, and maps typed operations to existing service, promotion, log, backup, key-rotation, firewall, and update workflows. Production systemd units socket-activate the root helper; tests and non-Linux development use the same policy through an in-process adapter.

**Tech Stack:** Go 1.24, Unix domain sockets, Linux `SO_PEERCRED`, systemd socket activation, nfpm, shell packaging hooks, Go unit/integration tests.

---

## File Structure

- Create `internal/privileged/types.go`: operation enum and typed request/response payloads.
- Create `internal/privileged/client.go`: narrow Panel-facing interface and JSON Unix client.
- Create `internal/privileged/server.go`: bounded one-request-per-connection server.
- Create `internal/privileged/policy.go`: unit, path, journal, backup, update, and firewall allowlists.
- Create `internal/privileged/adapter.go`: in-process executor used by tests and non-Linux builds.
- Create `internal/privileged/peercred_linux.go`: Linux `SO_PEERCRED` verification.
- Create `internal/privileged/peercred_other.go`: explicit unsupported socket-server behavior.
- Create `internal/privileged/executor.go`: typed mapping to existing privileged workflows.
- Create `internal/cli/helper.go`: `veil helper serve` command.
- Modify API apply, service, logs, backup, update, firewall, and key-rotation handlers to depend on the client interface.
- Create `packaging/systemd/veil-helper.service` and `packaging/systemd/veil-helper.socket`.
- Modify `packaging/systemd/veil.service` and renderer templates to run the Panel unprivileged.
- Modify nfpm and install/postinstall scripts to create the account, migrate ownership, and install/enable helper units.
- Add static policy, socket, migration, and end-to-end permission tests.

### Task 1: Typed Helper Contract

**Files:**
- Create: `internal/privileged/types.go`
- Create: `internal/privileged/client.go`
- Test: `internal/privileged/types_test.go`

- [x] **Step 1: Write failing request-contract tests**

Test JSON round trips and reject unknown operations. The supported operations are:

```go
const (
	OperationPromote        Operation = "promote"
	OperationServiceAction  Operation = "service_action"
	OperationServiceStatus  Operation = "service_status"
	OperationJournal        Operation = "journal"
	OperationBackupCreate   Operation = "backup_create"
	OperationBackupList     Operation = "backup_list"
	OperationBackupVerify   Operation = "backup_verify"
	OperationBackupPrune    Operation = "backup_prune"
	OperationBackupRestore  Operation = "backup_restore"
	OperationRotateKey      Operation = "rotate_key"
	OperationFirewallApply  Operation = "firewall_apply"
	OperationStageUpdate    Operation = "stage_update"
	OperationRestartPanel   Operation = "restart_panel"
)
```

Verify JSON never contains shell command strings and every request has `version`, `requestId`, `operation`, and exactly one typed payload.

- [x] **Step 2: Run contract tests and verify failure**

Run: `go test ./internal/privileged -run 'Contract|RoundTrip|UnknownOperation' -count=1`

Expected: FAIL because the package does not exist.

- [x] **Step 3: Define the narrow interface and payloads**

Define:

```go
type Client interface {
	Promote(context.Context, PromoteRequest) (PromoteResult, error)
	ServiceAction(context.Context, ServiceActionRequest) error
	ServiceStatus(context.Context, ServiceStatusRequest) (ServiceStatusResult, error)
	Journal(context.Context, JournalRequest) (JournalResult, error)
	Backup(context.Context, BackupRequest) (BackupResult, error)
	RotateKey(context.Context, RotateKeyRequest) error
	FirewallApply(context.Context, FirewallRequest) (FirewallResult, error)
	StageUpdate(context.Context, UpdateRequest) (UpdateResult, error)
	RestartPanel(context.Context) error
}
```

Use operation-specific structs with IDs, archive basenames, managed unit names, bounded line counts, and logical artifact IDs. Do not expose arbitrary executable names, command arguments, absolute caller-selected destinations, or passphrases.

- [x] **Step 4: Run package tests**

Run: `go test ./internal/privileged -count=1`

Expected: PASS.

- [x] **Step 5: Commit the protocol**

```bash
git add internal/privileged
git commit -m "feat: add privileged helper protocol"
```

### Task 2: Policy and In-Process Adapter

**Files:**
- Create: `internal/privileged/policy.go`
- Create: `internal/privileged/adapter.go`
- Create: `internal/privileged/executor.go`
- Test: `internal/privileged/policy_test.go`
- Test: `internal/privileged/adapter_test.go`

- [x] **Step 1: Write failing allowlist tests**

Cover:

- unknown and caller-crafted units are rejected;
- only units from the managed runtime catalog plus `veil.service` are accepted;
- archive names must be basenames ending in `.enc`;
- journal lines are clamped to `1..1000`;
- artifact IDs resolve through the generated artifact catalog;
- symlink and `..` escapes from `/var/lib/veil/staging` and `/etc/veil/generated` are rejected;
- backup operations cannot select arbitrary directories;
- update artifacts must resolve below `/var/lib/veil/updates`;
- no request can carry a shell command.

- [x] **Step 2: Run policy tests and verify failure**

Run: `go test ./internal/privileged -run 'Policy|Rejects|Allows' -count=1`

Expected: FAIL because policy enforcement is absent.

- [x] **Step 3: Implement policy-owned resolution**

Define:

```go
type Policy struct {
	StagingRoot  string
	GeneratedRoot string
	StateRoot    string
	BackupRoot   string
	UpdateRoot   string
	ManagedUnits map[string]struct{}
	Artifacts    map[string]ArtifactPath
}
```

Resolve paths using `filepath.Clean`, `filepath.Rel`, and `EvalSymlinks` for existing parents. Require the resulting relative path not to be absolute, `..`, or begin with `..` plus a separator. The policy, not the caller, selects state key, backup passphrase, firewall tooling, and executable paths.

- [x] **Step 4: Implement the local adapter**

The adapter validates every typed request with `Policy` and calls injected executor functions:

```go
type Executor struct {
	Promote       func(context.Context, ResolvedPromotion) (PromoteResult, error)
	ServiceAction func(context.Context, service.ActionRequest) error
	ServiceStatus func(context.Context, string) (ServiceStatusResult, error)
	Journal       func(context.Context, ResolvedJournal) (JournalResult, error)
	Backup        func(context.Context, ResolvedBackup) (BackupResult, error)
	RotateKey     func(context.Context) error
	Firewall      func(context.Context, ResolvedFirewall) (FirewallResult, error)
	Update        func(context.Context, ResolvedUpdate) (UpdateResult, error)
	RestartPanel  func(context.Context) error
}
```

Return stable error codes `invalid_request`, `forbidden_operation`, `not_found`, `conflict`, and `operation_failed`.

- [x] **Step 5: Run tests and commit**

Run: `go test ./internal/privileged -count=1`

Expected: PASS.

```bash
git add internal/privileged
git commit -m "feat: enforce privileged helper policy"
```

### Task 3: Unix Socket Transport and Peer Credentials

**Files:**
- Create: `internal/privileged/server.go`
- Create: `internal/privileged/peercred_linux.go`
- Create: `internal/privileged/peercred_other.go`
- Create: `internal/privileged/socket_client.go`
- Test: `internal/privileged/server_test.go`
- Test: `internal/privileged/socket_linux_test.go`

- [x] **Step 1: Write failing transport tests**

Test:

- one JSON request and one JSON response per connection;
- request body limit of 1 MiB;
- read/write deadlines from context;
- protocol version mismatch rejection;
- malformed and multi-payload requests rejection;
- socket path is not followed through a symlink;
- Linux accepts only the configured UID from `SO_PEERCRED`;
- Linux rejects an unapproved UID before decoding the request;
- non-Linux returns `ErrUnixPeerCredentialsUnsupported` from `ServeUnix`.

- [x] **Step 2: Run transport tests and verify failure**

Run: `go test ./internal/privileged -run 'Server|Socket|Peer' -count=1`

Expected: FAIL because the transport is absent.

- [x] **Step 3: Implement bounded JSON transport**

Use a `net.UnixListener`, `io.LimitReader(conn, 1<<20+1)`, `json.Decoder.DisallowUnknownFields`, and a response envelope:

```go
type ResponseEnvelope struct {
	Version   int             `json:"version"`
	RequestID string          `json:"requestId"`
	OK        bool            `json:"ok"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *Error          `json:"error,omitempty"`
}
```

Set deadlines, close after one response, and remove only a socket owned by the configured helper path.

- [x] **Step 4: Implement Linux peer verification**

Use `SyscallConn` and `unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)`. Compare `Uid` to the configured `veil` UID; allow root only in explicit test/admin configuration. Keep Linux code behind `//go:build linux` and the unsupported implementation behind `//go:build !linux`.

- [x] **Step 5: Run tests and commit**

Run:

```bash
go test ./internal/privileged -count=1
GOOS=linux GOARCH=amd64 go test -c ./internal/privileged
```

Expected: tests PASS and Linux test binary compiles.

```bash
git add internal/privileged go.mod go.sum
git commit -m "feat: secure privileged helper socket"
```

### Task 4: Helper CLI and Production Executor

**Files:**
- Create: `internal/cli/helper.go`
- Create: `internal/cli/helper_test.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/privileged/executor.go`
- Test: `internal/privileged/executor_test.go`

- [ ] **Step 1: Write failing CLI and executor tests**

Require `veil helper serve --socket /run/veil/helper.sock`, reject non-absolute socket paths, reject helper startup when not root on Linux, and verify each operation invokes only its injected existing workflow with resolved values.

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./internal/cli ./internal/privileged -run 'Helper|Executor' -count=1`

Expected: FAIL because helper command wiring is absent.

- [ ] **Step 3: Add the helper command**

Keep `helper` out of normal user-facing workflow help but include `veil helper serve --help`. Construct the policy from fixed defaults:

```text
/var/lib/veil/staging
/etc/veil/generated
/var/lib/veil
/var/lib/veil/backups
/var/lib/veil/updates
/run/veil/helper.sock
```

Resolve the configured `veil` account UID using `os/user`. Start the socket server with graceful shutdown on `SIGTERM` and `SIGINT`.

- [ ] **Step 4: Map operations to existing workflows**

Reuse generated-config promotion, managed systemd actions/status, bounded journal reads, backup create/list/verify/prune/restore, key rotation, firewall plans, verified update staging, and Panel restart. Preserve existing safety-copy, checksum, signature, and audit behavior. Do not execute a string assembled from a request.

- [ ] **Step 5: Run tests and commit**

Run: `go test ./internal/cli ./internal/privileged -count=1`

Expected: PASS.

```bash
git add internal/cli internal/privileged
git commit -m "feat: serve privileged helper operations"
```

### Task 5: Route Panel Privileged Operations Through the Client

**Files:**
- Modify: `internal/api/server.go`
- Modify: `internal/api/management_apply_context.go`
- Modify: `internal/api/live_config_promotion.go`
- Modify: `internal/api/services.go`
- Modify: `internal/api/panel_routes.go`
- Modify: `internal/api/management_backup.go`
- Modify: `internal/api/management_logs.go`
- Modify: `internal/api/management_firewall.go`
- Create: `internal/api/management_key_rotation.go`
- Modify: API tests for each handler

- [ ] **Step 1: Write failing boundary tests**

For every privileged Panel route, inject a recording `privileged.Client` and assert:

- apply uses logical artifact IDs and managed units;
- service control and status use allowlisted unit names;
- logs use bounded lines and managed units;
- backups never receive a passphrase from HTTP;
- update hands the helper a verified staged artifact ID;
- restart calls `RestartPanel`;
- key rotation is admin-only and revokes existing sessions;
- no API test observes direct `exec.Command("systemctl", ...)`;
- helper errors become the standard error envelope and structured audit outcome.

- [ ] **Step 2: Run API boundary tests and verify failure**

Run: `go test ./internal/api -run 'Privileged|Helper|RotateKey' -count=1`

Expected: FAIL because handlers call local privileged workflows directly.

- [ ] **Step 3: Inject one client dependency**

Add `Privileged privileged.Client` to API construction. Replace direct promotion, systemd, journal, backup, update, restart, firewall, and rotation execution with typed calls. Keep read-only state operations local when their paths are readable by `veil`.

- [ ] **Step 4: Preserve local development**

CLI server selection:

```go
if helperSocket != "" {
	client = privileged.NewSocketClient(helperSocket)
} else if developmentMode {
	client = privileged.NewLocalAdapter(policy, executor)
} else {
	return errors.New("privileged helper socket is required")
}
```

Production package defaults to `/run/veil/helper.sock`; tests inject a fake or adapter.

- [ ] **Step 5: Run API tests and commit**

Run: `go test ./internal/api ./internal/cli -count=1`

Expected: PASS.

```bash
git add internal/api internal/cli
git commit -m "feat: route panel operations through helper"
```

### Task 6: Unprivileged Panel and Hardened Helper Units

**Files:**
- Create: `packaging/systemd/veil-helper.service`
- Create: `packaging/systemd/veil-helper.socket`
- Modify: `packaging/systemd/veil.service`
- Modify: `internal/renderer/systemd.go`
- Modify: `internal/renderer/systemd_test.go`
- Modify: `packaging/nfpm.yaml`
- Test: `internal/packaging/systemd_test.go`

- [ ] **Step 1: Write failing static unit tests**

Require Panel unit:

```ini
User=veil
Group=veil
RuntimeDirectory=veil
Environment=VEIL_HELPER_SOCKET=/run/veil/helper.sock
CapabilityBoundingSet=
AmbientCapabilities=
ReadOnlyPaths=/etc/veil
ReadWritePaths=/var/lib/veil
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
```

Require helper socket:

```ini
ListenStream=/run/veil/helper.sock
SocketUser=root
SocketGroup=veil
SocketMode=0660
RemoveOnStop=true
```

Require helper service to run as root, accept only `AF_UNIX`, have no ambient capabilities, set `NoNewPrivileges=true`, and restrict writes to managed roots.

- [ ] **Step 2: Run unit tests and verify failure**

Run: `go test ./internal/renderer ./internal/packaging -run 'Systemd|Helper|Unprivileged' -count=1`

Expected: FAIL because helper units and `User=veil` are absent.

- [ ] **Step 3: Ship hardened static and rendered units**

Keep static package units and renderer output byte-for-byte aligned for security directives. Add `Requires=veil-helper.socket` and `After=network-online.target veil-helper.socket` to the Panel unit. The Panel retains network access but no Linux capabilities; the helper has no IP networking.

- [ ] **Step 4: Package helper units**

Add both units to nfpm contents, enable the socket rather than eagerly starting a permanent helper, and ensure uninstall removes only package-owned unit files, not state or backups.

- [ ] **Step 5: Run tests and commit**

Run: `go test ./internal/renderer ./internal/packaging -count=1`

Expected: PASS.

```bash
git add packaging/systemd packaging/nfpm.yaml internal/renderer internal/packaging
git commit -m "feat: run panel as unprivileged user"
```

### Task 7: Account Creation and Safe Ownership Migration

**Files:**
- Modify: `packaging/scripts/postinstall.sh`
- Modify: `packaging/scripts/preremove.sh`
- Modify: `scripts/install.sh`
- Modify: `scripts/uninstall.sh`
- Test: `tests/packaging/permissions_test.sh`
- Test: `tests/packaging/install_test.sh`

- [ ] **Step 1: Write failing packaging permission tests**

In a temporary root, assert scripts:

- create system group/user `veil` without a login shell or home;
- make `/etc/veil` `root:veil` mode `0750`;
- make `/etc/veil/state.key` `root:veil` mode `0640`;
- keep `/etc/veil/backup.passphrase` `root:root` mode `0600`;
- make `/var/lib/veil`, sessions, audit, staging, and updates `veil:veil`;
- make `/var/lib/veil/backups` root-managed and inaccessible to HTTP except through helper;
- create a timestamped safety copy before ownership changes;
- never delete the `veil` account or state on ordinary uninstall;
- enable `veil-helper.socket` before restarting `veil.service`.

- [ ] **Step 2: Run shell tests and verify failure**

Run: `bash tests/packaging/permissions_test.sh && bash tests/packaging/install_test.sh`

Expected: FAIL because the account and migration steps are absent.

- [ ] **Step 3: Implement idempotent migration**

Use `getent`, `groupadd --system`, and `useradd --system --gid veil --home-dir /nonexistent --shell /usr/sbin/nologin`. Before the first permission migration, create `/var/lib/veil/migrations/<UTC timestamp>/` and copy state metadata and key with preserved modes. Apply explicit `install -d`, `chown`, and `chmod`; do not recurse through symlinks.

- [ ] **Step 4: Verify upgrade and uninstall semantics**

Test fresh install, repeated postinstall, upgrade from root-owned v0.5 state, missing optional files, and uninstall. Confirm the Panel can read state/key and write sessions/audit/staging, but cannot write `/etc/veil/generated`, backup passphrase, package binaries, or systemd units.

- [ ] **Step 5: Run tests and commit**

Run:

```bash
bash -n packaging/scripts/postinstall.sh packaging/scripts/preremove.sh scripts/install.sh scripts/uninstall.sh
bash tests/packaging/permissions_test.sh
bash tests/packaging/install_test.sh
```

Expected: PASS.

```bash
git add packaging/scripts scripts tests/packaging
git commit -m "feat: migrate panel permissions safely"
```

### Task 8: Linux Socket and Permission Integration Tests

**Files:**
- Create: `tests/e2e/helper_socket_linux_test.go`
- Create: `tests/e2e/helper_permissions_linux_test.go`
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Write Linux-only end-to-end tests**

Run a helper on a temporary Unix socket and verify:

- approved UID can request a managed service status;
- different UID is rejected before execution;
- crafted unit, archive, artifact, and traversal requests are rejected;
- a valid staged artifact is promoted with a safety copy;
- Panel-user simulation cannot write live config;
- helper can write live config but cannot open an IP listener;
- oversize and malformed requests do not crash the helper.

- [ ] **Step 2: Run Linux tests and verify initial failure**

Run on Linux CI: `go test ./tests/e2e -run 'HelperSocket|HelperPermissions' -count=1`

Expected: FAIL until final socket ownership and executor wiring is complete.

- [ ] **Step 3: Add a dedicated CI job**

Run helper integration tests on Ubuntu with an ephemeral `veil` system user. The job must clean up its socket and temporary account even on failure and must not use production paths.

- [ ] **Step 4: Run full Linux-oriented verification**

Run:

```bash
go test ./...
GOOS=linux GOARCH=amd64 go test -c ./internal/privileged
GOOS=linux GOARCH=amd64 go test -c ./tests/e2e
```

Expected: PASS and both Linux test binaries compile locally; the integration job passes in GitHub Actions.

- [ ] **Step 5: Commit integration coverage**

```bash
git add tests/e2e .github/workflows/ci.yml
git commit -m "test: verify helper permissions and allowlists"
```

### Task 9: Security and Operations Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/hardening.md`
- Modify: `docs/operations.md`
- Modify: `docs/openapi.yaml`
- Modify: `CONTEXT.md`

- [ ] **Step 1: Write failing documentation checks**

Assert documentation names the `veil` user, helper socket, trust boundary, exact writable paths, root-required operations, migration safety copy, troubleshooting commands, and emergency rollback procedure.

- [ ] **Step 2: Run documentation checks and verify failure**

Run: `go test ./internal/packaging ./internal/api -run 'Documentation|OpenAPI' -count=1`

Expected: FAIL because the helper security model is undocumented.

- [ ] **Step 3: Document the privilege boundary**

Explain:

- Panel HTTP code runs as `veil`;
- helper is root and has no network listener;
- Unix peer credentials and socket ownership are both enforced;
- exact operations requiring root;
- paths readable/writable by Panel and helper;
- backup passphrases never cross the HTTP boundary;
- how to inspect `veil-helper.socket` and logs;
- how to roll back to the v0.5 unit using the migration safety copy;
- why adding arbitrary helper operations is a security-sensitive API change.

- [ ] **Step 4: Validate docs**

Run:

```bash
go test ./internal/packaging ./internal/api -run 'Documentation|OpenAPI' -count=1
npx --yes @redocly/cli lint docs/openapi.yaml
```

Expected: PASS.

- [ ] **Step 5: Commit documentation**

```bash
git add README.md CONTEXT.md docs
git commit -m "docs: document privileged helper boundary"
```

### Task 10: Complete Verification

**Files:**
- Modify only files required by discovered verification defects.

- [ ] **Step 1: Format and run all local checks**

Run:

```bash
gofmt -w internal tests
go vet ./...
go test ./...
npx --yes @redocly/cli lint docs/openapi.yaml
bash -n packaging/scripts/*.sh scripts/*.sh
```

Expected: PASS.

- [ ] **Step 2: Inspect binaries and units**

Run:

```bash
go build ./cmd/veil
go test ./internal/renderer ./internal/packaging -count=1
git diff --check
```

Expected: PASS and no whitespace errors.

- [ ] **Step 3: Run a security regression review**

Search for direct privileged execution reachable from Panel handlers:

```bash
rg -n 'exec\.Command|systemctl|journalctl|ufw|/etc/veil/generated|state\.key|backup\.passphrase' internal/api internal/panel
```

Expected: only non-executing constants, documentation strings, and tests remain; every real operation crosses `privileged.Client`.

- [ ] **Step 4: Verify GitHub Actions**

Push the branch and wait for unit, packaging, OpenAPI, shell, and Linux helper jobs to pass. Inspect logs for skipped helper tests and treat an unintended skip as failure.

- [ ] **Step 5: Commit any verification corrections**

```bash
git add -A
git commit -m "fix: complete privileged helper hardening"
```

Create this commit only when Step 1-4 required code or documentation corrections; otherwise leave the tree clean.
