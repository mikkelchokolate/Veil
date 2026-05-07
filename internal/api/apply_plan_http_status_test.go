package api

import (
	"net/http"
	"testing"
)

func TestApplyPlanHTTPStatusMapsValidityToStatus(t *testing.T) {
	policy := NewApplyPlanHTTPStatus()
	if policy.Status(ApplyPlanResponse{Valid: true}) != http.StatusOK {
		t.Fatal("valid plan should be 200")
	}
	if policy.Status(ApplyPlanResponse{Valid: false}) != http.StatusBadRequest {
		t.Fatal("invalid plan should be 400")
	}
}
