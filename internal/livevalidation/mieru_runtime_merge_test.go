package livevalidation

import (
	"context"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// TestValidatorMieruAllowsRuntimeIdentityMergingWithLegacyProfile locks in
// code-review round 2/3: BuildClientCredentials merges a runtime identity over
// the legacy profile with the same username (runtime replaces the profile),
// so a collision between a runtime identity and a legacy profile of the SAME
// inbound is the intended merge and must not be reported as a duplicate.
func TestValidatorMieruAllowsRuntimeIdentityMergingWithLegacyProfile(t *testing.T) {
	validator := testValidator()
	validator.Units = fakeUnitInspector{found: map[string]bool{"veil-mieru.service": true}}

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{Domain: "m.example.com"},
		Inbounds: []model.Inbound{
			{
				Name: "a", Protocol: "mieru", Transport: "tcp", Port: 3454, Enabled: true,
				Profiles: []model.ClientProfile{{Name: "alice", Username: "alice", Password: "pw1", Enabled: true}},
			},
		},
		RuntimeIdentities: map[string][]string{"a": {"alice"}},
	})

	if count := countIssueCode(response, "mieru_duplicate_username"); count != 0 {
		t.Fatalf("runtime identity merging with the same-inbound legacy profile must be valid: %+v", response.Issues)
	}
	if !response.Valid {
		t.Fatalf("expected valid config: %+v", response.Issues)
	}
}

// TestValidatorMieruRejectsDuplicateLegacyProfilesInOneInbound locks in the
// renderer contract: the aggregated mieru user list rejects any duplicate
// username, including two legacy profiles of the same inbound — the runtime
// merge exemption must not mask this (code-review round 2 P2).
func TestValidatorMieruRejectsDuplicateLegacyProfilesInOneInbound(t *testing.T) {
	validator := testValidator()
	validator.Units = fakeUnitInspector{found: map[string]bool{"veil-mieru.service": true}}

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{Domain: "m.example.com"},
		Inbounds: []model.Inbound{
			{
				Name: "a", Protocol: "mieru", Transport: "tcp", Port: 3454, Enabled: true,
				Profiles: []model.ClientProfile{
					{Name: "alice1", Username: "alice", Password: "pw1", Enabled: true},
					{Name: "alice2", Username: "alice", Password: "pw2", Enabled: true},
				},
			},
		},
	})

	if count := countIssueCode(response, "mieru_duplicate_username"); count != 1 {
		t.Fatalf("expected 1 duplicate mieru username issue, got %d: %+v", count, response.Issues)
	}
	if response.Valid {
		t.Fatalf("duplicate legacy profiles in one mieru inbound must be invalid")
	}
}

// TestValidatorMieruStillRejectsCrossInboundRuntimeCollision is the negative
// control: a runtime identity colliding with a profile on a DIFFERENT inbound
// is a real duplicate (both end up in the global user list).
func TestValidatorMieruStillRejectsCrossInboundRuntimeCollision(t *testing.T) {
	validator := testValidator()
	validator.Units = fakeUnitInspector{found: map[string]bool{"veil-mieru.service": true}}

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{Domain: "m.example.com"},
		Inbounds: []model.Inbound{
			{
				Name: "a", Protocol: "mieru", Transport: "tcp", Port: 3454, Enabled: true,
				Profiles: []model.ClientProfile{{Name: "alice", Username: "alice", Password: "pw1", Enabled: true}},
			},
			{
				Name: "b", Protocol: "mieru", Transport: "tcp", Port: 3455, Enabled: true,
			},
		},
		RuntimeIdentities: map[string][]string{"b": {"alice"}},
	})

	if count := countIssueCode(response, "mieru_duplicate_username"); count != 1 {
		t.Fatalf("expected 1 duplicate mieru username issue, got %d: %+v", count, response.Issues)
	}
}
