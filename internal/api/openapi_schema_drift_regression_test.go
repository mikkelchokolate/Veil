package api

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// schemaProperties returns the properties map of a named component schema.
func schemaProperties(t *testing.T, name string) map[string]any {
	t.Helper()
	document := loadOpenAPIMap(t)
	schemas := mapValue(t, document, "components")["schemas"].(map[string]any)
	schema, ok := schemas[name].(map[string]any)
	if !ok {
		t.Fatalf("components.schemas.%s missing", name)
	}
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		t.Fatalf("components.schemas.%s has no properties", name)
	}
	return props
}

// schemaRequired returns the required list of a named component schema.
func schemaRequired(t *testing.T, name string) []string {
	t.Helper()
	document := loadOpenAPIMap(t)
	schemas := mapValue(t, document, "components")["schemas"].(map[string]any)
	schema, ok := schemas[name].(map[string]any)
	if !ok {
		t.Fatalf("components.schemas.%s missing", name)
	}
	var required []string
	for _, item := range schema["required"].([]any) {
		required = append(required, fmt.Sprint(item))
	}
	sort.Strings(required)
	return required
}

// propertyEnum returns the enum values of one property of a named schema.
func propertyEnum(t *testing.T, schema, property string) []string {
	t.Helper()
	props := schemaProperties(t, schema)
	prop, ok := props[property].(map[string]any)
	if !ok {
		t.Fatalf("%s.%s missing", schema, property)
	}
	var values []string
	for _, item := range prop["enum"].([]any) {
		values = append(values, fmt.Sprint(item))
	}
	sort.Strings(values)
	return values
}

// TestOpenAPIApplySchemasMatchRuntime guards the apply contract fields and
// enums that drifted from the runtime models (issues #548-#550, #553, #555,
// #560-#562 and the RevisionView state enum of #547).
func TestOpenAPIApplySchemasMatchRuntime(t *testing.T) {
	// #548: honesty evidence fields on ApplyResponse.
	for _, field := range []string{
		"mutationStarted", "artifactsChanged", "servicesChanged", "firewallChanged",
		"artifactsRestored", "servicesRestored", "firewallRestored",
		"postRollbackHealthPass", "rollbackComplete", "ambiguous",
	} {
		if _, ok := schemaProperties(t, "ApplyResponse")[field]; !ok {
			t.Errorf("ApplyResponse missing honesty field %q", field)
		}
	}

	// #549: job status vocabulary must match internal/apply status constants.
	wantStatuses := []string{
		"applying", "failed", "health_check", "pending", "planning",
		"recovery_pending", "rollback_failed", "rolled_back", "rolling_back",
		"staged", "succeeded", "validating",
	}
	if got := propertyEnum(t, "ApplyJob", "status"); !equalStrings(got, wantStatuses) {
		t.Errorf("ApplyJob.status enum = %v, want %v", got, wantStatuses)
	}

	// #550: job evidence fields.
	for _, field := range []string{"operations", "ownerProcess", "leaseGeneration"} {
		if _, ok := schemaProperties(t, "ApplyJob")[field]; !ok {
			t.Errorf("ApplyJob missing field %q", field)
		}
	}

	// #555: only confirm is required; applyLive/applyServices default false.
	if got := schemaRequired(t, "ApplyRequest"); !equalStrings(got, []string{"confirm"}) {
		t.Errorf("ApplyRequest required = %v, want [confirm]", got)
	}

	// #562: planner only emits these operation types.
	wantOps := []string{"promote_file", "reload_service", "restart_service"}
	if got := propertyEnum(t, "ApplyOperation", "type"); !equalStrings(got, wantOps) {
		t.Errorf("ApplyOperation.type enum = %v, want %v", got, wantOps)
	}

	// #547: full derived system-state vocabulary.
	wantStates := []string{
		"applying", "degraded", "failed", "pending", "recovering", "rolled_back", "rolling_back", "synced", "untracked",
	}
	if got := propertyEnum(t, "RevisionView", "state"); !equalStrings(got, wantStates) {
		t.Errorf("RevisionView.state enum = %v, want %v", got, wantStates)
	}
	if got := propertyEnum(t, "ApplyStateResponse", "state"); !equalStrings(got, wantStates) {
		t.Errorf("ApplyStateResponse.state enum = %v, want %v", got, wantStates)
	}
	for _, field := range []string{
		"desiredRevision", "appliedRevision", "state", "activeJobId",
		"lastSuccessfulJobId", "lastFailedJobId", "lastError",
	} {
		if _, ok := schemaProperties(t, "ApplyStateResponse")[field]; !ok {
			t.Errorf("ApplyStateResponse missing field %q", field)
		}
	}
}

// TestOpenAPIMutationEnvelopesRequireSuccess guards #687: the mutation
// envelopes that carry the MutationOutcome honesty contract must keep
// "success" in their required list. With nullable-type codegen a dropped
// required turns Success into an omitempty *bool and the SPA/e2e honesty
// checks silently weaken while verify-sdk still greens.
func TestOpenAPIMutationEnvelopesRequireSuccess(t *testing.T) {
	for schema, want := range map[string][]string{
		"MutationOutcome":       {"revision", "success"},
		"ClientCreateResponse":  {"client", "revision", "success"},
		"ApplyRetryResponse":    {"applyJob", "revision", "success"},
		"ApplyRollbackResponse": {"desiredRevision", "revision", "selectedRevision", "success"},
		"KeyRotationResponse":   {"revokedSessions", "success"},
		"ServiceActionResponse": {"action", "service", "success"},
		"SuccessResponse":       {"success"},
	} {
		if got := schemaRequired(t, schema); !equalStrings(got, want) {
			t.Errorf("%s required = %v, want %v", schema, got, want)
		}
	}
}

// TestOpenAPIApplyEndpointsHaveResponseSchemas guards #551, #553, #560, #561:
// apply endpoints that used to return prose-only or wrong-shape bodies.
func TestOpenAPIApplyEndpointsHaveResponseSchemas(t *testing.T) {
	document := loadOpenAPIMap(t)
	paths := mapValue(t, document, "paths")

	jsonSchemaRef := func(path, method, status string) any {
		t.Helper()
		pathItem, _ := paths[path].(map[string]any)
		operation, _ := pathItem[method].(map[string]any)
		responses, _ := operation["responses"].(map[string]any)
		response, ok := responseByStatus(responses, status)
		if !ok {
			t.Fatalf("%s %s lacks response %s", strings.ToUpper(method), path, status)
		}
		mapped, _ := response.(map[string]any)
		content, _ := mapped["content"].(map[string]any)
		jsonContent, _ := content["application/json"].(map[string]any)
		return jsonContent["schema"]
	}

	for _, tc := range []struct {
		path, method string
		wantRef      string
	}{
		{"/api/apply/state", "get", "#/components/schemas/ApplyStateResponse"},
		{"/api/apply/jobs/{id}", "get", "#/components/schemas/ApplyJob"},
		{"/api/apply/jobs/{id}/retry", "post", "#/components/schemas/ApplyRetryResponse"},
		{"/api/apply/reconcile", "post", "#/components/schemas/ApplyReconcileResponse"},
		{"/api/apply/rollback", "post", "#/components/schemas/ApplyRollbackResponse"},
	} {
		schema, _ := jsonSchemaRef(tc.path, tc.method, "200").(map[string]any)
		if got := fmt.Sprint(schema["$ref"]); got != tc.wantRef {
			t.Errorf("%s %s 200 schema = %q, want %q", tc.method, tc.path, got, tc.wantRef)
		}
	}

	// #553: honesty error statuses carry ApplyResponse bodies, not only envelopes.
	for _, status := range []string{"400", "422"} {
		schema, _ := jsonSchemaRef("/api/apply", "post", status).(map[string]any)
		oneOf, _ := schema["oneOf"].([]any)
		var refs []string
		for _, option := range oneOf {
			mapped, _ := option.(map[string]any)
			refs = append(refs, fmt.Sprint(mapped["$ref"]))
		}
		sort.Strings(refs)
		want := []string{"#/components/schemas/ApplyResponse", "#/components/schemas/ErrorEnvelope"}
		if !equalStrings(refs, want) {
			t.Errorf("POST /api/apply %s oneOf = %v, want %v", status, refs, want)
		}
	}
}

// TestOpenAPIRemainingDriftFields guards the schema fields reported in
// issues #552, #556-#559, #366 and the mutation envelope of #554.
func TestOpenAPIRemainingDriftFields(t *testing.T) {
	// #552: restore job status set and evidence fields.
	wantJobStatuses := []string{"degraded", "failed", "pending", "queued", "running", "succeeded"}
	if got := propertyEnum(t, "BackupRestoreJob", "status"); !equalStrings(got, wantJobStatuses) {
		t.Errorf("BackupRestoreJob.status enum = %v, want %v", got, wantJobStatuses)
	}
	for _, field := range []string{"outcome", "phase", "restored", "httpStatus"} {
		if _, ok := schemaProperties(t, "BackupRestoreJob")[field]; !ok {
			t.Errorf("BackupRestoreJob missing field %q", field)
		}
	}

	// #556: soft warning on create responses.
	if _, ok := schemaProperties(t, "BackupCreateResponse")["warning"]; !ok {
		t.Error("BackupCreateResponse missing warning")
	}

	// #557: per-inbound hysteria2 insecure flag.
	if _, ok := schemaProperties(t, "Inbound")["hysteria2Insecure"]; !ok {
		t.Error("Inbound missing hysteria2Insecure")
	}

	// #558: restart affordances on service status.
	for _, field := range []string{"actionName", "restartable"} {
		if _, ok := schemaProperties(t, "ServiceStatus")[field]; !ok {
			t.Errorf("ServiceStatus missing field %q", field)
		}
	}

	// #559: effective auth method.
	if got := propertyEnum(t, "AuthStatusResponse", "authMethod"); !equalStrings(got, []string{"dev-anonymous", "static-token"}) {
		t.Errorf("AuthStatusResponse.authMethod enum = %v, want [dev-anonymous static-token]", got)
	}

	// #366: update is an async durable job; 202 carries jobId, not success.
	updateProps := schemaProperties(t, "UpdateResponse")
	for _, field := range []string{"jobId", "status", "staged", "installed", "version", "message"} {
		if _, ok := updateProps[field]; !ok {
			t.Errorf("UpdateResponse missing field %q", field)
		}
	}
	for _, required := range schemaRequired(t, "UpdateResponse") {
		if required == "success" {
			t.Error("UpdateResponse still requires success; live API returns 202+jobId")
		}
	}
}
