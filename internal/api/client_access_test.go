package api

import "testing"

func TestClientAccessBuildsClientLinksAndRendererUsersFromProfiles(t *testing.T) {
	access, err := BuildClientAccess(Settings{Domain: "example.com", Stack: "both", NaiveUsername: "global", NaivePassword: "global-pass"}, Inbound{
		Name:      "naive",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		Enabled:   true,
		Profiles:  []ClientProfile{{Name: "alice", Username: "alice-user", Password: "alice-pass", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("BuildClientAccess: %v", err)
	}
	links := access.ClientLinks()
	if len(links) != 1 || links[0].Name != "naive/alice" || links[0].URI == "" {
		t.Fatalf("unexpected links: %+v", links)
	}
	users := access.NaiveUsers()
	if len(users) != 1 || users[0].Username != "alice-user" || users[0].Password != "alice-pass" {
		t.Fatalf("unexpected naive users: %+v", users)
	}
}
