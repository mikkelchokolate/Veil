package clientaccess

import "testing"

func TestBuildClientSubscriptionRejectsInvalidFormat(t *testing.T) {
	_, err := BuildClientSubscription(ClientLinksResponse{}, "json")
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}
