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

func TestEnvironmentResolvesMetricsAccessPolicy(t *testing.T) {
	env := NewEnvironment()

	access, source := env.MetricsAccess("authenticated")
	if access != "authenticated" || source != "--metrics-access" {
		t.Fatalf("metrics access = %q %q", access, source)
	}

	required, err := env.MetricsAuthRequired("auto", true, "VEIL_API_TOKEN", true)
	if err != nil {
		t.Fatalf("auto metrics policy: %v", err)
	}
	if !required {
		t.Fatalf("auto metrics policy should require auth on public listen")
	}

	required, err = env.MetricsAuthRequired("public", false, "disabled", false)
	if err != nil {
		t.Fatalf("local public metrics policy: %v", err)
	}
	if required {
		t.Fatalf("public metrics policy should not require auth on local listen")
	}

	if _, err := env.MetricsAuthRequired("public", true, "VEIL_API_TOKEN", true); err == nil {
		t.Fatalf("expected public metrics policy to be rejected on public listen")
	}
}
