package warp

import "testing"

func TestPolicyAppliesDefaultsAndRedactsSecrets(t *testing.T) {
	cfg := Config{PrivateKey: "private", LicenseKey: "license"}
	SetDefaults(&cfg)
	if cfg.Endpoint != "engage.cloudflareclient.com:2408" || cfg.SocksListen != "127.0.0.1" || cfg.SocksPort != 40000 || cfg.MTU != 1280 {
		t.Fatalf("defaults = %+v", cfg)
	}
	redacted := Redact(cfg)
	if redacted.PrivateKey != "[REDACTED]" || redacted.LicenseKey != "[REDACTED]" {
		t.Fatalf("redacted = %+v", redacted)
	}
}
