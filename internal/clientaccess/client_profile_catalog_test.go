package clientaccess

import "testing"

func TestClientProfileCatalogGeneratesMissingPasswords(t *testing.T) {
	catalog := NewClientProfileCatalogWithPasswordGenerator([]ClientProfile{
		{Name: "alice", Username: "alice", Enabled: true},
	}, func() string { return "generated-pass" })

	profiles := catalog.WithCompletedPasswords(nil)
	if got := profiles[0].Password; got != "generated-pass" {
		t.Fatalf("generated password = %q", got)
	}
}

func TestClientProfileCatalogPreservesExistingPasswords(t *testing.T) {
	catalog := NewClientProfileCatalogWithPasswordGenerator([]ClientProfile{
		{Name: "alice", Username: "alice", Enabled: true},
	}, func() string { return "generated-pass" })

	profiles := catalog.WithCompletedPasswords([]ClientProfile{
		{Name: "alice", Username: "alice", Password: "existing-pass", Enabled: true},
	})
	if got := profiles[0].Password; got != "existing-pass" {
		t.Fatalf("preserved password = %q", got)
	}
}

func TestClientProfileCatalogEnabledProfilesOnly(t *testing.T) {
	catalog := NewClientProfileCatalog([]ClientProfile{
		{Name: "alice", Enabled: true},
		{Name: "bob", Enabled: false},
	})

	profiles := catalog.Enabled()
	if len(profiles) != 1 || profiles[0].Name != "alice" {
		t.Fatalf("enabled profiles = %+v", profiles)
	}
}
