package clientaccess

import "testing"

func TestClientAccessHysteria2UsersFromProfiles(t *testing.T) {
	access, err := BuildClientAccess(Settings{Domain: "example.com"}, Inbound{
		Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true,
		Profiles: []ClientProfile{
			{Name: "alice", Username: "alice-user", Password: "alice-pass", Enabled: true},
			{Name: "bob", Username: "bob-user", Password: "bob-pass", Enabled: true},
		},
	})
	if err != nil {
		t.Fatalf("BuildClientAccess: %v", err)
	}
	users := access.Hysteria2Users()
	if len(users) != 2 {
		t.Fatalf("want 2 hysteria2 users, got %+v", users)
	}
	if users[0].Username != "alice-user" || users[0].Password != "alice-pass" {
		t.Fatalf("user[0] = %+v", users[0])
	}
	if users[1].Username != "bob-user" || users[1].Password != "bob-pass" {
		t.Fatalf("user[1] = %+v", users[1])
	}
}

func TestClientProfileCatalogListReturnsAllProfiles(t *testing.T) {
	profiles := []ClientProfile{
		{Name: "a", Username: "a", Password: "pa", Enabled: true},
		{Name: "b", Username: "b", Password: "pb", Enabled: false},
	}
	catalog := NewClientProfileCatalog(profiles)

	all := catalog.List()
	if len(all) != 2 {
		t.Fatalf("List() should return all profiles, got %+v", all)
	}
	// List returns a copy: mutating it must not affect the catalog.
	all[0].Name = "mutated"
	if again := catalog.List(); again[0].Name != "a" {
		t.Fatalf("List() must return a copy, got %+v", again)
	}
	// Enabled() filters; List() does not.
	if enabled := catalog.Enabled(); len(enabled) != 1 || enabled[0].Name != "a" {
		t.Fatalf("Enabled() = %+v", enabled)
	}
}
