# Live Validation and Apply Preview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Validate candidate Panel configuration against live host state and show a structured, secret-free apply preview before any mutation.

**Architecture:** A new `internal/livevalidation` package owns deterministic validation and accepts injectable host probes for ports, DNS, binaries, and systemd units. API save/apply paths call the same validator authoritatively, while the UI uses a debounced validation endpoint for immediate feedback. Apply plans retain their compatibility string fields and add structured issues and operations derived from the generated artifact catalog.

**Tech Stack:** Go 1.24, `net`, `os/exec`, existing Veil HTTP/model/apply-plan packages, embedded HTML/CSS/JavaScript, OpenAPI 3.1, Go tests, Redocly.

---

## File Structure

- Create `internal/livevalidation/types.go`: validator request, issue, dependency interfaces, and stable issue codes.
- Create `internal/livevalidation/validator.go`: pure candidate checks and host-probe orchestration.
- Create `internal/livevalidation/host_probes.go`: production TCP/UDP, DNS, binary, and systemd probes.
- Create `internal/livevalidation/validator_test.go`: deterministic unit coverage with fake probes.
- Create `internal/api/management_validation.go`: API request parsing, RBAC, and validation handler.
- Create `internal/api/management_validation_test.go`: endpoint and authorization tests.
- Modify `internal/model/types.go`: shared validation issues and structured apply operations.
- Modify `internal/applyplan/planner.go`: attach issues and structured operations.
- Modify `internal/api/apply_plan_builder.go`: pass live validation and managed artifact context into planning.
- Modify `internal/api/management_apply_intent.go`: expose candidate validation and reject invalid save/apply requests.
- Modify `internal/api/management_inbounds.go`: run authoritative validation before persistence.
- Modify `internal/api/management_settings.go`: run authoritative validation before persistence.
- Modify `internal/panel/panel_inbound_actions.go`: debounce validation, map issues to fields, and block invalid submission.
- Modify `internal/panel/panel_apply_card.go`: render structured operations, risks, rollback state, and validation provenance.
- Modify `internal/panel/panel.css`: responsive validation and preview presentation.
- Modify `docs/openapi.yaml`: document validation and structured apply-plan payloads with examples.
- Modify `docs/operations.md`: explain validation guarantees and limitations.

### Task 1: Shared Validation Model

**Files:**
- Modify: `internal/model/types.go`
- Test: `internal/model/types_test.go`

- [x] **Step 1: Write the failing JSON contract test**

Add a test that marshals an `ApplyPlanResponse` containing a `ValidationIssue` and `ApplyOperation`, then asserts the JSON keys `issues`, `operations`, `interruptionRisk`, `rollbackAvailable`, and `validationSource` are present and secrets are not introduced.

```go
func TestApplyPlanResponseIncludesStructuredPreview(t *testing.T) {
	value := ApplyPlanResponse{
		Valid: true,
		Issues: []ValidationIssue{{
			Code: "port_in_use", Severity: "error", Field: "port",
			Message: "TCP port 443 is already in use",
		}},
		Operations: []ApplyOperation{{
			Type: "promote_file", Destination: "/etc/veil/generated/caddy/Caddyfile",
			InterruptionRisk: "reload", RollbackAvailable: true,
			ValidationSource: "live-host",
		}},
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`"issues"`, `"operations"`, `"interruptionRisk"`,
		`"rollbackAvailable"`, `"validationSource"`,
	} {
		if !bytes.Contains(data, []byte(key)) {
			t.Fatalf("missing %s in %s", key, data)
		}
	}
}
```

- [x] **Step 2: Run the contract test and verify failure**

Run: `go test ./internal/model -run TestApplyPlanResponseIncludesStructuredPreview -count=1`

Expected: FAIL because the structured types and fields do not exist.

- [x] **Step 3: Add stable shared types**

Add:

```go
type ValidationIssue struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Field       string `json:"field,omitempty"`
	InboundID   string `json:"inboundId,omitempty"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
	Source      string `json:"source"`
}

type ApplyOperation struct {
	Type              string `json:"type"`
	Source            string `json:"source,omitempty"`
	Destination       string `json:"destination,omitempty"`
	Unit              string `json:"unit,omitempty"`
	InterruptionRisk  string `json:"interruptionRisk"`
	RollbackAvailable bool   `json:"rollbackAvailable"`
	ValidationSource  string `json:"validationSource"`
}
```

Extend `ApplyPlanResponse` with:

```go
Issues     []ValidationIssue `json:"issues"`
Operations []ApplyOperation `json:"operations"`
```

- [x] **Step 4: Run model tests**

Run: `go test ./internal/model -count=1`

Expected: PASS.

- [x] **Step 5: Commit the model contract**

```bash
git add internal/model/types.go internal/model/types_test.go
git commit -m "feat: add structured validation model"
```

### Task 2: Deterministic Live Validator

**Files:**
- Create: `internal/livevalidation/types.go`
- Create: `internal/livevalidation/validator.go`
- Test: `internal/livevalidation/validator_test.go`

- [x] **Step 1: Write failing duplicate, required-field, and host-probe tests**

Cover:

```go
func TestValidatorRejectsDuplicateEnabledBindings(t *testing.T)
func TestValidatorAllowsUnchangedBindingOwnedByCandidate(t *testing.T)
func TestValidatorRejectsNewTCPAndUDPBindingsInUse(t *testing.T)
func TestValidatorReportsMissingDomainEmailCredentialBinaryAndUnit(t *testing.T)
func TestValidatorSortsIssuesDeterministically(t *testing.T)
```

Use fakes implementing:

```go
type PortProbe interface {
	Available(context.Context, string, int) (bool, error)
}
type DNSResolver interface {
	LookupHost(context.Context, string) ([]string, error)
}
type BinaryLookup interface {
	LookPath(string) (string, error)
}
type UnitInspector interface {
	Exists(context.Context, string) (bool, error)
}
```

The unchanged-binding test supplies both `CurrentInbounds` and an identical candidate. A new candidate using a busy port must receive `port_in_use`.

- [x] **Step 2: Run validator tests and verify failure**

Run: `go test ./internal/livevalidation -count=1`

Expected: FAIL because the package does not exist.

- [x] **Step 3: Implement the validator**

Define:

```go
type Request struct {
	Settings        model.Settings
	Inbounds        []model.Inbound
	CurrentInbounds []model.Inbound
	Warp            model.WarpConfig
}

type Response struct {
	Valid     bool                    `json:"valid"`
	Issues    []model.ValidationIssue `json:"issues"`
	CheckedAt time.Time               `json:"checkedAt"`
}

type Validator struct {
	Ports    PortProbe
	DNS      DNSResolver
	Binaries BinaryLookup
	Units    UnitInspector
	Now      func() time.Time
}
```

Implement stable checks:

- enabled inbound requires name, supported protocol, transport, and port `1..65535`;
- enabled `(transport, port)` pairs are unique;
- a busy host port is allowed only when the current enabled inbound owns the same protocol, transport, port, and identity;
- protocols requiring TLS report missing domain/email/certificate material with field paths;
- configured domain is resolved and reports `dns_unresolved` on failure;
- every enabled protocol maps to its required runtime binary and managed unit;
- issue severities are `error`, `warning`, or `info`;
- issues are sorted by severity, inbound ID, field, and code;
- `Valid` is false exactly when an `error` issue exists.

- [x] **Step 4: Run validator tests**

Run: `go test ./internal/livevalidation -count=1`

Expected: PASS.

- [x] **Step 5: Commit the validator**

```bash
git add internal/livevalidation
git commit -m "feat: add live configuration validation"
```

### Task 3: Production Host Probes

**Files:**
- Create: `internal/livevalidation/host_probes.go`
- Test: `internal/livevalidation/host_probes_test.go`

- [x] **Step 1: Write failing real TCP/UDP probe tests**

Reserve an ephemeral TCP listener and UDP packet socket, then assert the matching transport is unavailable and a released port becomes available.

```go
func TestHostPortProbeDetectsBusyTCPPort(t *testing.T)
func TestHostPortProbeDetectsBusyUDPPort(t *testing.T)
func TestHostPortProbeRejectsUnknownTransport(t *testing.T)
```

Also test binary lookup and a fake command runner for `systemctl show --property=LoadState --value`.

- [x] **Step 2: Run probe tests and verify failure**

Run: `go test ./internal/livevalidation -run 'TestHost|TestSystemd' -count=1`

Expected: FAIL because production probes are missing.

- [x] **Step 3: Implement bounded host probes**

Use `net.Listen("tcp", "127.0.0.1:"+port)` and `net.ListenPacket("udp", "127.0.0.1:"+port)` with immediate close. Use `net.DefaultResolver.LookupHost`, `exec.LookPath`, and an injected context-aware command runner for unit inspection. Unknown transports return an error; probe errors become validation issues instead of panics.

- [x] **Step 4: Run package tests**

Run: `go test ./internal/livevalidation -count=1`

Expected: PASS on Windows and Linux.

- [x] **Step 5: Commit host probes**

```bash
git add internal/livevalidation/host_probes.go internal/livevalidation/host_probes_test.go
git commit -m "feat: probe live host configuration"
```

### Task 4: Validation API and Authoritative Save Checks

**Files:**
- Create: `internal/api/management_validation.go`
- Create: `internal/api/management_validation_test.go`
- Modify: `internal/api/management_apply_intent.go`
- Modify: `internal/api/management_inbounds.go`
- Modify: `internal/api/management_settings.go`
- Modify: `internal/api/server.go`

- [x] **Step 1: Write failing endpoint and mutation tests**

Add tests proving:

- `POST /api/validation` requires an authenticated editor or admin session;
- read-only viewer receives `403`;
- malformed payload receives the standard JSON error envelope;
- a busy candidate port yields `200` with `valid:false` and `port_in_use`;
- saving the same invalid inbound yields `422` and does not mutate state;
- applying invalid state yields `422` before staging or service actions;
- unchanged persisted bindings remain saveable.

- [x] **Step 2: Run focused API tests and verify failure**

Run: `go test ./internal/api -run 'Validation|RejectsInvalid.*Save|RejectsInvalid.*Apply' -count=1`

Expected: FAIL because the route and authoritative validator hook are absent.

- [x] **Step 3: Add one validator dependency to the API**

Introduce an API-owned interface:

```go
type configurationValidator interface {
	Validate(context.Context, livevalidation.Request) livevalidation.Response
}
```

Parse:

```go
type validationRequest struct {
	Settings model.Settings    `json:"settings"`
	Inbounds []model.Inbound   `json:"inbounds"`
	Warp     model.WarpConfig  `json:"warp"`
}
```

Register `POST /api/validation`, load current inbounds for unchanged-binding ownership, and return the shared response. Call the identical helper before settings/inbound persistence and before apply execution. Return `422` with the existing error envelope plus structured issues.

- [x] **Step 4: Run API tests**

Run: `go test ./internal/api -count=1`

Expected: PASS.

- [x] **Step 5: Commit validation enforcement**

```bash
git add internal/api
git commit -m "feat: enforce live validation before mutation"
```

### Task 5: Structured Apply Preview

**Files:**
- Modify: `internal/applyplan/planner.go`
- Modify: `internal/applyplan/planner_test.go`
- Modify: `internal/api/apply_plan_builder.go`
- Modify: `internal/api/apply_plan_builder_test.go`
- Modify: `internal/api/management_apply_intent.go`
- Modify: `internal/generatedconfig/artifact_catalog.go`

- [x] **Step 1: Write failing operation-plan tests**

Assert a representative multi-protocol plan contains:

```go
[]model.ApplyOperation{
	{
		Type: "promote_file",
		Source: "/var/lib/veil/staging/caddy/Caddyfile",
		Destination: "/etc/veil/generated/caddy/Caddyfile",
		InterruptionRisk: "reload",
		RollbackAvailable: true,
		ValidationSource: "render-and-live-host",
	},
	{
		Type: "restart_service",
		Unit: "veil-caddy@edge.service",
		InterruptionRisk: "connection-drop",
		RollbackAvailable: true,
		ValidationSource: "managed-unit-catalog",
	},
}
```

Verify operation order is deterministic and no credential values, passwords, private keys, or tokens appear after JSON marshaling.

- [x] **Step 2: Run apply-plan tests and verify failure**

Run: `go test ./internal/applyplan ./internal/api -run 'Structured|Operation|SecretFree' -count=1`

Expected: FAIL because plans only expose strings.

- [x] **Step 3: Build operations from managed artifacts and units**

Extend planner input with generated and live roots, artifact descriptors, and validation issues. Produce:

- one `promote_file` operation per generated artifact;
- one `reload_service` or `restart_service` operation per affected managed unit;
- `update_firewall` only when the candidate port set changes;
- `remove_file` and `disable_service` for deleted enabled inbounds;
- rollback availability only when a safety copy is produced;
- validation provenance from render, host, and catalog checks.

Keep `Configs`, `Actions`, and `Runtimes` for clients predating v0.6.

- [x] **Step 4: Run planner and API tests**

Run: `go test ./internal/applyplan ./internal/api -count=1`

Expected: PASS.

- [x] **Step 5: Commit structured preview**

```bash
git add internal/applyplan internal/api internal/generatedconfig
git commit -m "feat: add structured apply preview"
```

### Task 6: Debounced Validation UI

**Files:**
- Modify: `internal/panel/panel_inbound_actions.go`
- Modify: `internal/panel/panel_template.go`
- Modify: `internal/panel/panel.css`
- Test: `internal/panel/panel_test.go`

- [x] **Step 1: Write failing panel contract tests**

Assert rendered Panel assets contain:

- `role="status"` and `aria-live="polite"` validation summary;
- a 300 ms debounce;
- `aria-invalid` and `aria-describedby` field wiring;
- `AbortController` to cancel stale requests;
- submit disabling only while validation is pending or has errors;
- remediation text without exposing credentials.

- [x] **Step 2: Run focused panel tests and verify failure**

Run: `go test ./internal/panel -run 'Validation|InboundModal' -count=1`

Expected: FAIL because live validation UI is absent.

- [x] **Step 3: Implement accessible validation feedback**

Serialize the complete candidate snapshot, debounce `POST /api/validation` by 300 ms, abort stale calls, and ignore responses whose sequence number is no longer current. Map `field` paths to inputs, set `aria-invalid`, and render one concise remediation per issue in an `aria-live="polite"` region. Preserve all values when the server rejects save.

- [x] **Step 4: Run panel tests**

Run: `go test ./internal/panel -count=1`

Expected: PASS.

- [x] **Step 5: Commit validation UX**

```bash
git add internal/panel
git commit -m "feat: add live validation interface"
```

### Task 7: Structured Preview UI

**Files:**
- Modify: `internal/panel/panel_apply_card.go`
- Modify: `internal/panel/apply_workflow_command_catalog.go`
- Modify: `internal/panel/panel.css`
- Test: `internal/panel/panel_test.go`

- [x] **Step 1: Write failing preview-rendering tests**

Require operation type labels, destination/unit, interruption risk, rollback state, validation source, warning summary, an empty-state message, and compatibility fallback for old string-only responses.

- [x] **Step 2: Run focused tests and verify failure**

Run: `go test ./internal/panel -run 'ApplyPreview|StructuredOperation' -count=1`

Expected: FAIL because the UI infers metadata from string actions.

- [x] **Step 3: Render structured operations**

Render operation rows directly from `plan.operations`; use the existing string catalog only when `operations` is absent. Group file, service, firewall, and removal operations; use text plus icons; announce validation errors; keep the apply button disabled when `valid` is false; and show rollback availability and connection-drop risk before confirmation.

- [x] **Step 4: Run panel tests**

Run: `go test ./internal/panel -count=1`

Expected: PASS.

- [x] **Step 5: Commit preview UX**

```bash
git add internal/panel
git commit -m "feat: render structured apply preview"
```

### Task 8: OpenAPI and Operator Documentation

**Files:**
- Modify: `docs/openapi.yaml`
- Modify: `docs/operations.md`
- Modify: `README.md`
- Test: `internal/api/openapi_contract_test.go`

- [x] **Step 1: Write failing OpenAPI contract assertions**

Assert `/api/validation`, `ValidationIssue`, `ValidationResponse`, and `ApplyOperation` exist, all response codes reference the common error envelope, and examples include a busy port and a connection-drop preview.

- [x] **Step 2: Run documentation checks and verify failure**

Run: `go test ./internal/api -run OpenAPI -count=1`

Expected: FAIL because new schemas and path are undocumented.

- [x] **Step 3: Document contracts and guarantees**

Document request/response schemas, RBAC, issue codes, examples, authoritative server validation, race limitations between preview and apply, and how Veil revalidates immediately before mutation.

- [x] **Step 4: Validate documentation**

Run:

```bash
go test ./internal/api -run OpenAPI -count=1
npx --yes @redocly/cli lint docs/openapi.yaml
```

Expected: both commands PASS with no OpenAPI errors.

- [x] **Step 5: Commit documentation**

```bash
git add docs/openapi.yaml docs/operations.md README.md internal/api/openapi_contract_test.go
git commit -m "docs: document live validation and previews"
```

### Task 9: End-to-End and Responsive Verification

**Files:**
- Modify: `test/e2e/live_validation_preview_test.go`
- Modify: `test/e2e/proxy_negative_test.go`

- [x] **Step 1: Add failing end-to-end scenarios**

Cover authenticated validation, viewer denial, busy-port save refusal, unchanged-binding acceptance, structured preview, secret redaction, and apply refusal without state mutation.

- [x] **Step 2: Run new end-to-end tests and verify failure**

Run: `go test -tags e2e ./test/e2e/... -run 'Validation|StructuredPreview' -count=1`

Expected: FAIL before the scenarios are wired to the final server.

- [x] **Step 3: Complete server wiring**

Construct production probes in the CLI server, inject them into the API, and keep deterministic fake probes in all existing tests. Ensure validation timeouts are bounded by the request context.

- [x] **Step 4: Run full verification**

Run:

```bash
gofmt -w internal/livevalidation internal/model internal/api internal/applyplan internal/panel test/e2e
go test ./...
npx --yes @redocly/cli lint docs/openapi.yaml
```

Expected: PASS.

Open the Panel in the in-app browser at desktop and 390x844 mobile sizes. Verify the inbound modal has no horizontal overflow, stale validation responses do not replace current results, keyboard focus reaches every error, and the apply preview remains readable.

- [x] **Step 5: Commit end-to-end coverage**

```bash
git add internal test/e2e
git commit -m "test: cover live validation workflow"
```
