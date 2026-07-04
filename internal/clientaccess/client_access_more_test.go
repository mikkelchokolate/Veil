package clientaccess

import "testing"

func TestBuildClientAccessReturnsCredentialError(t *testing.T) {
	_, err := BuildClientAccess(Settings{Domain: "example.com"}, Inbound{
		Name:      "naive",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		Enabled:   true,
		Profiles:  []ClientProfile{{Name: "alice", Enabled: true}},
	})
	if err == nil {
		t.Fatal("expected error for invalid client credentials")
	}
}
