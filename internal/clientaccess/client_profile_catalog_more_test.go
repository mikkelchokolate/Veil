package clientaccess

import "testing"

func TestNewClientProfileCatalogWithNilGeneratorUsesDefault(t *testing.T) {
	catalog := NewClientProfileCatalogWithPasswordGenerator([]ClientProfile{{Name: "alice", Enabled: true}}, nil)
	profiles, err := catalog.WithCompletedPasswords(nil)
	if err != nil {
		t.Fatalf("WithCompletedPasswords: %v", err)
	}
	if len(profiles) != 1 || profiles[0].Password == "" {
		t.Fatalf("expected generated password, got %+v", profiles)
	}
}
