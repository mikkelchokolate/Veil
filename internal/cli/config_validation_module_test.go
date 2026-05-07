package cli

import "testing"

func TestConfigValidationModuleValidatesSnapshotBytes(t *testing.T) {
	body := []byte(`{"settings":{"panelListen":"127.0.0.1:2096","stack":"both","mode":"server"},"inbounds":[],"routingRules":[],"warp":{"enabled":false}}`)
	result, err := NewConfigValidation().ValidateBytes(body)
	if err != nil {
		t.Fatalf("ValidateBytes: %v", err)
	}
	if !result.Valid || len(result.Errors) != 0 {
		t.Fatalf("result = %+v", result)
	}
}
