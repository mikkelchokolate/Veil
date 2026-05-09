package inbounds

import "testing"

func TestInboundCloneDeepCopiesClientProfiles(t *testing.T) {
	original := []Inbound{{Name: "naive", Profiles: []ClientProfile{{Name: "alice", Password: "secret"}}}}
	cloned := NewInboundClone().Slice(original)
	cloned[0].Profiles[0].Password = "mutated"
	if original[0].Profiles[0].Password != "secret" {
		t.Fatalf("original profile was mutated: %+v", original)
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
