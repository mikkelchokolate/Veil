package managementstate

import (
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestCompleteSetupForAdminsMarksCompleted(t *testing.T) {
	snapshot := model.ManagementSnapshot{
		Users: []model.User{{Username: "veil", Role: "admin"}},
	}
	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	CompleteSetupForAdmins(&snapshot, at)
	if !snapshot.Setup.Completed || snapshot.Setup.CompletedAt != "2026-08-20T12:00:00Z" {
		t.Fatalf("setup = %+v", snapshot.Setup)
	}
}

func TestCompleteSetupForAdminsIgnoresViewerOnly(t *testing.T) {
	snapshot := model.ManagementSnapshot{
		Users: []model.User{{Username: "read", Role: "viewer"}},
	}
	CompleteSetupForAdmins(&snapshot, time.Now())
	if snapshot.Setup.Completed {
		t.Fatal("viewer-only state must stay first-run")
	}
}

func TestDecodeCurrentSchemaHealsAdminSetupCompleted(t *testing.T) {
	inputJSON := `{
		"schemaVersion": 4,
		"settings": {"panelListen": "127.0.0.1:2096", "mode": "server"},
		"inbounds": [],
		"routingRules": [],
		"warp": {},
		"setup": {"completed": false},
		"users": [
			{"username": "admin", "passwordHash": "hash", "role": "admin"}
		]
	}`
	snapshot, err := NewManagementStateCodec().Decode([]byte(inputJSON))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !snapshot.Setup.Completed {
		t.Fatalf("expected healed setup completion, got %+v", snapshot.Setup)
	}
}
