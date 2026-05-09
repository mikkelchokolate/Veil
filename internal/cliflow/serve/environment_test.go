package serve

import "testing"

func TestEnvironmentResolvesAuthPathsAndWebBasePath(t *testing.T) {
	env := NewEnvironment()
	token, tokenSource := env.AuthToken("secret")
	statePath, stateSource := env.StatePath("/state.json")
	webBasePath, webBasePathSource := env.WebBasePath("secret")
	if token != "secret" || tokenSource != "--auth-token" {
		t.Fatalf("auth = %q %q", token, tokenSource)
	}
	if statePath != "/state.json" || stateSource != "--state" {
		t.Fatalf("state = %q %q", statePath, stateSource)
	}
	if webBasePath != "/secret/" || webBasePathSource != "--web-base-path" {
		t.Fatalf("web base path = %q %q", webBasePath, webBasePathSource)
	}
}
