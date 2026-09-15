package livevalidation

import (
	"context"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestValidatorMieruNormalizedIdentityEqualToInboundNameIsNotDuplicate(t *testing.T) {
	validator := testValidator()
	validator.Units = fakeUnitInspector{found: map[string]bool{"veil-mieru.service": true}}

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{Domain: "m.example.com"},
		Inbounds: []model.Inbound{{
			Name: "alpha", Protocol: "mieru", Transport: "tcp", Port: 3454, Enabled: true,
		}},
		RuntimeIdentities: map[string][]string{"alpha": {"alpha"}},
	})
	if count := countIssueCode(response, "mieru_duplicate_username"); count != 0 {
		t.Fatalf("normalized identity equal to inbound name must not be a self-duplicate: %+v", response.Issues)
	}
}

func TestValidatorMieruDoesNotInventUnusedFallbackCollision(t *testing.T) {
	validator := testValidator()
	validator.Units = fakeUnitInspector{found: map[string]bool{"veil-mieru.service": true}}

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{Domain: "m.example.com"},
		Inbounds: []model.Inbound{
			{Name: "alpha", Protocol: "mieru", Transport: "tcp", Port: 3454, Enabled: true},
			{Name: "beta", Protocol: "mieru", Transport: "tcp", Port: 3455, Enabled: true},
		},
		RuntimeIdentities: map[string][]string{
			"alpha": {"beta"},
			"beta":  {"other"},
		},
	})
	if count := countIssueCode(response, "mieru_duplicate_username"); count != 0 {
		t.Fatalf("normalized identity must not collide with an unused fallback: %+v", response.Issues)
	}
}

func TestValidatorMieruAllDisabledProfilesDoNotInventFallbackDuplicate(t *testing.T) {
	validator := testValidator()
	validator.Units = fakeUnitInspector{found: map[string]bool{"veil-mieru.service": true}}

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{},
		Inbounds: []model.Inbound{
			{
				Name: "tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true,
				Profiles: []model.ClientProfile{{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: true}},
			},
			{
				Name: "alice", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true, Password: "leftover",
				Profiles: []model.ClientProfile{{Name: "bob", Username: "bob", Password: "bob-pass", Enabled: false}},
			},
		},
	})
	if count := countIssueCode(response, "mieru_duplicate_username"); count != 0 {
		t.Fatalf("all-disabled inbound must not invent inbound-name fallback: %+v", response.Issues)
	}
}

func TestValidatorMieruAllDisabledProfilesRequireCredential(t *testing.T) {
	validator := testValidator()
	validator.Units = fakeUnitInspector{found: map[string]bool{"veil-mieru.service": true}}

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{},
		Inbounds: []model.Inbound{{
			Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "leftover",
			Profiles: []model.ClientProfile{{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: false}},
		}},
	})
	if response.Valid {
		t.Fatal("all-disabled profiles with leftover inbound password must not be valid")
	}
	if count := countIssueCode(response, "credential_required"); count == 0 {
		t.Fatalf("expected credential_required, got %+v", response.Issues)
	}
}

func TestValidatorMieruStillRejectsTrueNormalizedCollision(t *testing.T) {
	validator := testValidator()
	validator.Units = fakeUnitInspector{found: map[string]bool{"veil-mieru.service": true}}

	response := validator.Validate(context.Background(), Request{
		Settings: model.Settings{Domain: "m.example.com"},
		Inbounds: []model.Inbound{
			{Name: "alpha", Protocol: "mieru", Transport: "tcp", Port: 3454, Enabled: true},
			{Name: "beta", Protocol: "mieru", Transport: "tcp", Port: 3455, Enabled: true},
		},
		RuntimeIdentities: map[string][]string{
			"alpha": {"shared"},
			"beta":  {"shared"},
		},
	})
	if count := countIssueCode(response, "mieru_duplicate_username"); count != 1 {
		t.Fatalf("expected 1 true collision, got %d: %+v", count, response.Issues)
	}
}
