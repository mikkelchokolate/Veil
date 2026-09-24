package inbounds

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestInboundCloneDeepCopiesClientProfiles(t *testing.T) {
	original := []Inbound{{Name: "naive", Profiles: []ClientProfile{{Name: "alice", Password: "secret"}}}}
	cloned := NewInboundClone().Slice(original)
	cloned[0].Profiles[0].Password = "mutated"
	if original[0].Profiles[0].Password != "secret" {
		t.Fatalf("original profile was mutated: %+v", original)
	}
}

// TestInboundCloneDeepCopiesProtocolFields covers #945: ProtocolFields is a
// map, so a struct copy aliases it. Mutating the clone — top level AND nested
// containers — must never change the source, and vice versa.
func TestInboundCloneDeepCopiesProtocolFields(t *testing.T) {
	original := []Inbound{{
		Name: "dyn",
		ProtocolFields: map[string]any{
			"mode":    "relay",
			"options": map[string]any{"cipher": "aes", "nested": map[string]any{"depth": float64(2)}},
			"tags":    []any{"a", "b"},
		},
		RuntimeCredentials: []model.RuntimeCredential{{Name: "c1", Username: "u", Password: "p"}},
	}}
	cloned := NewInboundClone().Slice(original)

	// Top-level and nested mutations on the clone stay in the clone.
	cloned[0].ProtocolFields["mode"] = "mutated"
	cloned[0].ProtocolFields["options"].(map[string]any)["cipher"] = "mutated"
	cloned[0].ProtocolFields["options"].(map[string]any)["nested"].(map[string]any)["depth"] = float64(99)
	cloned[0].ProtocolFields["tags"].([]any)[0] = "mutated"
	cloned[0].RuntimeCredentials[0].Password = "mutated"

	if original[0].ProtocolFields["mode"] != "relay" {
		t.Fatalf("original protocol field mutated: %v", original[0].ProtocolFields)
	}
	if original[0].ProtocolFields["options"].(map[string]any)["cipher"] != "aes" {
		t.Fatalf("original nested protocol map mutated: %v", original[0].ProtocolFields)
	}
	if original[0].ProtocolFields["options"].(map[string]any)["nested"].(map[string]any)["depth"] != float64(2) {
		t.Fatalf("original deeply-nested protocol map mutated: %v", original[0].ProtocolFields)
	}
	if original[0].ProtocolFields["tags"].([]any)[0] != "a" {
		t.Fatalf("original protocol slice mutated: %v", original[0].ProtocolFields)
	}
	if original[0].RuntimeCredentials[0].Password != "p" {
		t.Fatalf("original runtime credentials mutated: %+v", original[0].RuntimeCredentials)
	}

	// And the source must not be able to write through to the clone either.
	original[0].ProtocolFields["options"].(map[string]any)["cipher"] = "source-mutation"
	if cloned[0].ProtocolFields["options"].(map[string]any)["cipher"] != "mutated" {
		t.Fatalf("clone observed source mutation: %v", cloned[0].ProtocolFields)
	}
}

func TestInboundCatalogListDoesNotExposeProfileSlices(t *testing.T) {
	catalog := NewInboundCatalog([]Inbound{{Name: "naive", Profiles: []ClientProfile{{Name: "alice", Password: "secret"}}}})
	listed := catalog.List()
	listed[0].Profiles[0].Password = "mutated"
	listedAgain := catalog.List()
	if listedAgain[0].Profiles[0].Password != "secret" {
		t.Fatalf("catalog profile was mutated: %+v", listedAgain)
	}
}
