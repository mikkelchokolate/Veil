package clientaccess

import "testing"

func TestNewClientProfileCatalogWithNilGeneratorUsesDefault(t *testing.T) {
	catalog := NewClientProfileCatalogWithPasswordGenerator([]ClientProfile{{Name: "alice", Enabled: true}}, nil)
	profiles := catalog.WithCompletedPasswords(nil)
	if len(profiles) != 1 || profiles[0].Password == "" {
		t.Fatalf("expected generated password, got %+v", profiles)
	}
}
