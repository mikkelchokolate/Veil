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
		"settings":{"panelListen":"127.0.0.1:2096","panelAccess":"caddy","webBasePath":"/panel/","mode":"dev","domain":"panel.example.com","email":"admin@example.com"},
		"inbounds":[{"name":"mieru-tcp","protocol":"mieru","transport":"tcp","port":443,"enabled":true,"password":"secret"}],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`)

	result, err := NewManagementStateValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if result.Valid || !containsString(result.Errors, "panel caddy access uses 443/tcp; choose another TCP port for inbound mieru-tcp") {
		t.Fatalf("expected Apply plan intent error, got %+v", result)
	}
}
