package api

import "testing"

func TestRouterSecurityRecognizesBearerToken(t *testing.T) {
	if got := bearerToken("Bearer secret-token"); got != "secret-token" {
		t.Fatalf("bearerToken() = %q", got)
	}
}
