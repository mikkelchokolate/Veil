package api

import (
	"os"
	"strings"
	"testing"
)

func TestOpenAPIDocumentsLiveValidationAndStructuredApplyPreview(t *testing.T) {
	body, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI: %v", err)
	}
	document := string(body)
	for _, required := range []string{
		"  /api/validation:",
		"$ref: \"#/components/schemas/ValidationRequest\"",
		"$ref: \"#/components/schemas/ValidationResponse\"",
		"    ValidationIssue:",
		"    ApplyOperation:",
		"    ValidationFailure:",
		"port_in_use",
		"connection-drop",
		"rollbackAvailable",
		"validationSource",
	} {
		if !strings.Contains(document, required) {
			t.Fatalf("OpenAPI missing %q", required)
		}
	}
}

func TestOpenAPIApplyPlanRequiresStructuredCollections(t *testing.T) {
	body, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatalf("read OpenAPI: %v", err)
	}
	document := string(body)
	start := strings.Index(document, "    ApplyPlanResponse:")
	if start < 0 {
		t.Fatal("ApplyPlanResponse schema missing")
	}
	section := document[start:]
	if next := strings.Index(section[len("    ApplyPlanResponse:"):], "\n    ApplyRequest:"); next >= 0 {
		section = section[:len("    ApplyPlanResponse:")+next]
	}
	for _, required := range []string{
		"required: [valid, configs, actions, issues, operations]",
		"issues:",
		"$ref: \"#/components/schemas/ValidationIssue\"",
		"operations:",
		"$ref: \"#/components/schemas/ApplyOperation\"",
	} {
		if !strings.Contains(section, required) {
			t.Fatalf("ApplyPlanResponse missing %q:\n%s", required, section)
		}
	}
}
