package api

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIOperationsDeclareRolesAndProductionErrors(t *testing.T) {
	document := loadOpenAPIMap(t)
	paths := mapValue(t, document, "paths")
	criticalMutations := map[string]struct{}{
		"POST /api/apply":                                 {},
		"POST /api/apply/rollback":                        {},
		"PUT /api/settings":                               {},
		"POST /api/inbounds":                              {},
		"POST /api/v1/clients":                            {},
		"PATCH /api/v1/clients/{id}":                      {},
		"POST /api/v1/clients/{id}/bindings":              {},
		"PATCH /api/v1/clients/{id}/bindings/{bindingId}": {},
		"POST /api/v1/clients/{id}/tokens":                {},
		"POST /api/backups/{name}/restore":                {},
	}
	seenCritical := map[string]bool{}
	for path, rawPath := range paths {
		pathItem, _ := rawPath.(map[string]any)
		for _, method := range []string{"get", "head", "post", "put", "patch", "delete"} {
			rawOperation, ok := pathItem[method]
			if !ok {
				continue
			}
			operation, _ := rawOperation.(map[string]any)
			roles, ok := operation["x-roles"].([]any)
			if !ok || len(roles) == 0 {
				t.Errorf("%s %s lacks non-empty x-roles metadata", strings.ToUpper(method), path)
			}
			key := strings.ToUpper(method) + " " + path
			if _, critical := criticalMutations[key]; !critical {
				continue
			}
			seenCritical[key] = true
			responses, _ := operation["responses"].(map[string]any)
			for _, status := range []string{"409", "422", "423", "503"} {
				response, ok := responseByStatus(responses, status)
				if !ok {
					t.Errorf("%s lacks documented %s response", key, status)
					continue
				}
				if !responseHasExampleOrReference(response) {
					t.Errorf("%s response %s lacks concrete example/reference", key, status)
				}
			}
		}
	}
	for key := range criticalMutations {
		if !seenCritical[key] {
			t.Errorf("critical OpenAPI mutation not found: %s", key)
		}
	}
}

func TestOpenAPIDocumentsOperationalHeaders(t *testing.T) {
	document := loadOpenAPIMap(t)
	components := mapValue(t, document, "components")
	headers := mapValue(t, components, "headers")
	for _, name := range []string{"Location", "ETag", "Idempotency-Replayed", "Retry-After", "Last-Event-ID"} {
		if _, ok := headers[name]; !ok {
			t.Errorf("components.headers lacks %s", name)
		}
	}
	paths := mapValue(t, document, "paths")
	assertOperationHeader(t, paths, "/api/v1/clients", "post", "201", "Location")
	assertOperationHeader(t, paths, "/api/v1/clients", "post", "201", "Idempotency-Replayed")
	assertOperationHeader(t, paths, "/api/v1/clients/{id}", "patch", "200", "ETag")
	assertOperationHeader(t, paths, "/api/v1/clients/{id}", "patch", "200", "Idempotency-Replayed")
	assertOperationHeader(t, paths, "/api/v1/events", "get", "200", "Last-Event-ID")
	assertOperationHeader(t, paths, "/api/v1/events", "get", "429", "Retry-After")
}

func loadOpenAPIMap(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func mapValue(t *testing.T, value map[string]any, key string) map[string]any {
	t.Helper()
	mapped, ok := value[key].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI %s is not a map", key)
	}
	return mapped
}

func responseByStatus(responses map[string]any, status string) (any, bool) {
	for key, value := range responses {
		if fmt.Sprint(key) == status {
			return value, true
		}
	}
	return nil, false
}

func responseHasExampleOrReference(value any) bool {
	mapped, _ := value.(map[string]any)
	if _, ok := mapped["$ref"]; ok {
		return true
	}
	serialized := fmt.Sprint(mapped)
	return strings.Contains(serialized, "example") || strings.Contains(serialized, "examples")
}

func assertOperationHeader(t *testing.T, paths map[string]any, path, method, status, header string) {
	t.Helper()
	pathItem, _ := paths[path].(map[string]any)
	operation, _ := pathItem[method].(map[string]any)
	responses, _ := operation["responses"].(map[string]any)
	response, ok := responseByStatus(responses, status)
	if !ok {
		t.Errorf("%s %s lacks response %s", strings.ToUpper(method), path, status)
		return
	}
	mapped, _ := response.(map[string]any)
	responseHeaders, _ := mapped["headers"].(map[string]any)
	if _, ok := responseHeaders[header]; !ok {
		t.Errorf("%s %s response %s lacks %s header", strings.ToUpper(method), path, status, header)
	}
}
