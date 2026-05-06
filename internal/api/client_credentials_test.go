package api

import "testing"

func TestBuildClientCredentialsNormalizesEnabledClientProfiles(t *testing.T) {
	credentials, err := BuildClientCredentials(Inbound{Profiles: []ClientProfile{
		{Name: "alice", Password: "alice-pass", Enabled: true},
		{Name: "bob", Username: "bobby", Password: "bob-pass", Enabled: true},
		{Name: "disabled", Password: "disabled-pass", Enabled: false},
	}})
	if err != nil {
		t.Fatalf("BuildClientCredentials: %v", err)
	}
	if len(credentials) != 2 {
		t.Fatalf("credentials = %+v", credentials)
	}
	if credentials[0] != (ClientCredential{Name: "alice", Username: "alice", Password: "alice-pass"}) {
		t.Fatalf("first credential = %+v", credentials[0])
	}
	if credentials[1] != (ClientCredential{Name: "bob", Username: "bobby", Password: "bob-pass"}) {
		t.Fatalf("second credential = %+v", credentials[1])
	}
}

func TestBuildClientCredentialsRejectsMissingUsernameOrPassword(t *testing.T) {
	for _, inbound := range []Inbound{
		{Profiles: []ClientProfile{{Name: "", Username: "", Password: "pass", Enabled: true}}},
		{Profiles: []ClientProfile{{Name: "alice", Username: "alice", Password: "", Enabled: true}}},
	} {
		if _, err := BuildClientCredentials(inbound); err == nil {
			t.Fatalf("expected invalid Client credential error for %+v", inbound)
		}
	}
}
