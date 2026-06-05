package serve

import (
	"strings"
	"testing"
)

func TestExposurePolicyRejectsDirectPublicHTTP(t *testing.T) {
	err := NewExposurePolicy().Validate(ExposureInput{
		PanelAccess:           "direct",
		PublicListen:          true,
		TokenConfigured:       true,
		SessionAuthConfigured: true,
		MetricsAuthRequired:   true,
	})
	if err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("expected TLS refusal, got %v", err)
	}
}

func TestExposurePolicyRejectsDirectWithoutToken(t *testing.T) {
	err := NewExposurePolicy().Validate(ExposureInput{
		PanelAccess:           "direct",
		PublicListen:          true,
		SessionAuthConfigured: true,
		MetricsAuthRequired:   true,
		NativeTLS:             true,
	})
	if err == nil || !strings.Contains(err.Error(), "API token") {
		t.Fatalf("expected API token refusal, got %v", err)
	}
}

func TestExposurePolicyTreatsCaddyAsPublicWithoutRequiringAPIToken(t *testing.T) {
	policy := NewExposurePolicy()
	err := policy.Validate(ExposureInput{
		PanelAccess:           "caddy",
		MetricsAuthRequired:   true,
		ProxyTLS:              true,
		TokenConfigured:       false,
		SessionAuthConfigured: false,
	})
	if err == nil || !strings.Contains(err.Error(), "user/session") {
		t.Fatalf("expected Caddy exposure without session auth to fail, got %v", err)
	}

	err = policy.Validate(ExposureInput{
		PanelAccess:           "caddy",
		MetricsAuthRequired:   true,
		ProxyTLS:              true,
		TokenConfigured:       false,
		SessionAuthConfigured: true,
	})
	if err != nil {
		t.Fatalf("expected Caddy exposure with session auth to pass without API token: %v", err)
	}
}

func TestExposurePolicyRejectsPublicMetrics(t *testing.T) {
	err := NewExposurePolicy().Validate(ExposureInput{
		PanelAccess:           "caddy",
		SessionAuthConfigured: true,
		ProxyTLS:              true,
	})
	if err == nil || !strings.Contains(err.Error(), "metrics") {
		t.Fatalf("expected metrics refusal, got %v", err)
	}
}

func TestExposurePolicyAllowsLocalFirstRun(t *testing.T) {
	err := NewExposurePolicy().Validate(ExposureInput{PanelAccess: "local"})
	if err != nil {
		t.Fatalf("local first run: %v", err)
	}
}

func TestExposurePolicyAllowsExplicitUnsafeHTTPOverride(t *testing.T) {
	err := NewExposurePolicy().Validate(ExposureInput{
		PanelAccess:           "direct",
		PublicListen:          true,
		TokenConfigured:       true,
		SessionAuthConfigured: true,
		MetricsAuthRequired:   true,
		AllowUnsafePublicHTTP: true,
	})
	if err != nil {
		t.Fatalf("explicit unsafe override: %v", err)
	}
}
