package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestV1ClientPatchPreservesEveryOmittedDurableField(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	created := createFullyPopulatedClient(t, r, "patch-preserve")
	id := created["id"].(string)
	version := int(created["version"].(float64))

	response := v1Request(t, r, http.MethodPatch, "/api/v1/clients/"+id,
		`{"version":`+strconv.Itoa(version)+`,"name":"renamed-only"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("PATCH status=%d body=%s", response.Code, response.Body.String())
	}
	updated := decodeJSONMap(t, response.Body.Bytes())
	if updated["name"] != "renamed-only" {
		t.Fatalf("name=%v want renamed-only", updated["name"])
	}
	assertDurableClientFieldsEqual(t, created, updated, map[string]bool{"name": true, "version": true, "updatedAt": true})
}

func TestV1ClientPatchExplicitNullClearsNullableFields(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	created := createFullyPopulatedClient(t, r, "patch-clear")
	id := created["id"].(string)
	version := int(created["version"].(float64))

	response := v1Request(t, r, http.MethodPatch, "/api/v1/clients/"+id,
		`{"version":`+strconv.Itoa(version)+`,"email":null,"groupId":null,"quotaBytes":null,"quotaResetPolicy":null,"quotaResetAt":null,"expiresAt":null,"deviceLimit":null,"notes":null}`)
	if response.Code != http.StatusOK {
		t.Fatalf("PATCH status=%d body=%s", response.Code, response.Body.String())
	}
	updated := decodeJSONMap(t, response.Body.Bytes())
	for _, field := range []string{"email", "groupId", "quotaBytes", "quotaResetAt", "expiresAt", "deviceLimit"} {
		if value, exists := updated[field]; exists && value != nil {
			t.Errorf("%s=%v; explicit null must clear it", field, value)
		}
	}
	if policy, _ := updated["quotaResetPolicy"].(string); policy != "never" {
		t.Errorf("quotaResetPolicy=%v; explicit null must canonicalize to %q", updated["quotaResetPolicy"], "never")
	}
	if notes, exists := updated["notes"]; exists && notes != "" && notes != nil {
		t.Errorf("notes=%v; explicit null must clear it", notes)
	}
	assertDurableClientFieldsEqual(t, created, updated, map[string]bool{
		"email": true, "groupId": true, "quotaBytes": true, "quotaResetPolicy": true, "quotaResetAt": true,
		"expiresAt": true, "deviceLimit": true, "notes": true, "version": true, "updatedAt": true,
	})
}

func TestV1ClientEnablePatchDoesNotRewriteUnrelatedFields(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	created := createFullyPopulatedClient(t, r, "patch-enable")
	id := created["id"].(string)
	version := int(created["version"].(float64))

	response := v1Request(t, r, http.MethodPatch, "/api/v1/clients/"+id,
		`{"version":`+strconv.Itoa(version)+`,"enabled":false}`)
	if response.Code != http.StatusOK {
		t.Fatalf("PATCH status=%d body=%s", response.Code, response.Body.String())
	}
	updated := decodeJSONMap(t, response.Body.Bytes())
	if updated["enabled"] != false {
		t.Fatalf("enabled=%v want false", updated["enabled"])
	}
	assertDurableClientFieldsEqual(t, created, updated, map[string]bool{"enabled": true, "version": true, "updatedAt": true, "status": true})
}

func TestClientPatchOpenAPIAndGeneratedContractIncludesEveryDurableField(t *testing.T) {
	body, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	spec := string(body)
	pathStart := strings.Index(spec, "  /api/v1/clients/{id}:\n")
	pathEnd := strings.Index(spec[pathStart+1:], "\n  /api/v1/clients/{id}/")
	if pathStart < 0 || pathEnd < 0 {
		t.Fatal("client-by-id OpenAPI path not found")
	}
	pathBlock := spec[pathStart : pathStart+1+pathEnd]
	if !strings.Contains(pathBlock, "\n    patch:\n") || strings.Contains(pathBlock, "\n    put:\n") {
		t.Fatalf("client update must be PATCH-only; path block:\n%s", pathBlock)
	}
	if !strings.Contains(pathBlock, "#/components/schemas/ClientPatchRequest") {
		t.Fatal("PATCH operation must use ClientPatchRequest with field-presence semantics")
	}

	for _, schemaName := range []string{"ClientPatchRequest", "ClientView"} {
		block := openAPISchemaBlock(t, spec, schemaName)
		for _, field := range []string{
			"name", "email", "enabled", "groupId", "quotaBytes", "quotaResetPolicy",
			"quotaResetAt", "expiresAt", "deviceLimit", "notes",
		} {
			if !strings.Contains(block, "\n        "+field+":\n") {
				t.Errorf("schema %s omits durable field %s", schemaName, field)
			}
		}
	}
}

func createFullyPopulatedClient(t *testing.T, r http.Handler, name string) map[string]any {
	t.Helper()
	response := v1Request(t, r, http.MethodPost, "/api/v1/clients", `{
		"name":"`+name+`",
		"email":"owner@example.test",
		"enabled":true,
		"groupId":"group-production",
		"quotaBytes":987654321,
		"quotaResetPolicy":"weekly",
		"quotaResetAt":1893456000,
		"expiresAt":1924992000,
		"notes":"durable notes"
	}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	return unwrapClient(t, response.Body.Bytes())
}

func decodeJSONMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertDurableClientFieldsEqual(t *testing.T, before, after map[string]any, allowedChanges map[string]bool) {
	t.Helper()
	for _, field := range []string{
		"id", "name", "email", "enabled", "groupId", "quotaBytes", "quotaResetPolicy",
		"quotaResetAt", "expiresAt", "deviceLimit", "notes", "depleted", "createdAt",
		"updatedAt", "version", "status",
	} {
		if allowedChanges[field] {
			continue
		}
		beforeJSON, _ := json.Marshal(before[field])
		afterJSON, _ := json.Marshal(after[field])
		if string(beforeJSON) != string(afterJSON) {
			t.Errorf("durable field %s changed: before=%s after=%s", field, beforeJSON, afterJSON)
		}
	}
}

func openAPISchemaBlock(t *testing.T, spec, name string) string {
	t.Helper()
	marker := "    " + name + ":\n"
	start := strings.Index(spec, marker)
	if start < 0 {
		t.Fatalf("OpenAPI schema %s not found", name)
	}
	rest := spec[start+len(marker):]
	end := len(rest)
	for i := 0; i < len(rest); {
		lineEnd := strings.IndexByte(rest[i:], '\n')
		if lineEnd < 0 {
			break
		}
		lineEnd += i
		line := rest[i:lineEnd]
		if strings.HasPrefix(line, "    ") && len(line) > 4 && line[4] != ' ' {
			end = i
			break
		}
		i = lineEnd + 1
	}
	return rest[:end]
}
