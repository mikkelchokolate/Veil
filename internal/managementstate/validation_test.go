package managementstate

import (
	"strings"
	"testing"
)

func TestValidationRejectsUnknownLegacyStateFields(t *testing.T) {
	body := []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","stack":"both"},
		"inbounds":[],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`)

	result, err := NewValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if result.Valid || len(result.Errors) == 0 || !strings.Contains(result.Errors[0], `json: unknown field "stack"`) {
		t.Fatalf("expected strict codec error, got %+v", result)
	}
}

func TestValidationChecksManagementSnapshotShape(t *testing.T) {
	body := []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev"},
		"inbounds":[
			{"name":"a","protocol":"mieru","transport":"tcp","port":443,"enabled":true},
			{"name":"b","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true}
		],
		"routingRules":[{"name":"","match":"geoip:private","outbound":"direct","enabled":true}],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`)

	result, err := NewValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if result.Valid || !containsError(result.Errors, "inbounds[1]: duplicate transport/port tcp:443") || !containsError(result.Errors, "routingRules[0].name is required") {
		t.Fatalf("unexpected validation result: %+v", result)
	}
}

func containsError(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
