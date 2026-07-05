package api

import (
	"strings"
	"testing"
)

func TestManagementStateValidationUsesStrictStateCodec(t *testing.T) {
	body := []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","stack":"both"},
		"inbounds":[],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`)

	result, err := NewManagementStateValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if result.Valid || len(result.Errors) == 0 || !strings.Contains(result.Errors[0], `json: unknown field "stack"`) {
		t.Fatalf("expected strict codec stack rejection, got %+v", result)
	}
}

func TestManagementStateValidationReusesApplyPlanIntent(t *testing.T) {
	body := []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","panelAccess":"caddy","webBasePath":"/panel/","mode":"dev","panelDomain":"panel.example.com","panelEmail":"admin@example.com"},
		"inbounds":[{"name":"mieru-tcp","protocol":"mieru","transport":"tcp","port":443,"enabled":true,"password":"secret"}],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`)

	result, err := NewManagementStateValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected invalid plan, got %+v", result)
	}
	found := false
	for _, err := range result.Errors {
		if strings.Contains(err, "is claimed by multiple owners") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected bind conflict error, got %+v", result.Errors)
	}
}
