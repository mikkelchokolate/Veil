package warp

import (
	"strings"
	"testing"
)

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

func TestSetDefaultsDoesNotOverrideExistingValues(t *testing.T) {
	cfg := Config{
		Endpoint:    "custom.endpoint:1234",
		SocksListen: "0.0.0.0",
		SocksPort:   1111,
		MTU:         1420,
	}
	SetDefaults(&cfg)
	if cfg.Endpoint != "custom.endpoint:1234" || cfg.SocksListen != "0.0.0.0" || cfg.SocksPort != 1111 || cfg.MTU != 1420 {
		t.Fatalf("existing values overwritten = %+v", cfg)
	}
}

func TestRedactLeavesEmptySecretsEmpty(t *testing.T) {
	cfg := Config{PrivateKey: "", LicenseKey: ""}
	redacted := Redact(cfg)
	if redacted.PrivateKey != "" || redacted.LicenseKey != "" {
		t.Fatalf("empty secrets should stay empty, got %+v", redacted)
	}
}

func TestPreserveRedactedKeepsCurrentValue(t *testing.T) {
	current := Config{LicenseKey: "current-license", PrivateKey: "current-private"}
	update := Config{LicenseKey: "[REDACTED]", PrivateKey: "[REDACTED]"}
	result := PreserveRedacted(update, current)
	if result.LicenseKey != "current-license" || result.PrivateKey != "current-private" {
		t.Fatalf("expected current values to be preserved, got %+v", result)
	}
}

func TestPreserveRedactedUsesUpdateValue(t *testing.T) {
	current := Config{LicenseKey: "current-license", PrivateKey: "current-private"}
	update := Config{LicenseKey: "new-license", PrivateKey: "new-private"}
	result := PreserveRedacted(update, current)
	if result.LicenseKey != "new-license" || result.PrivateKey != "new-private" {
		t.Fatalf("expected update values to be used, got %+v", result)
	}
}

func TestPreserveRedactedMixedUpdate(t *testing.T) {
	current := Config{LicenseKey: "current-license", PrivateKey: "current-private"}
	update := Config{LicenseKey: "[REDACTED]", PrivateKey: "new-private"}
	result := PreserveRedacted(update, current)
	if result.LicenseKey != "current-license" || result.PrivateKey != "new-private" {
		t.Fatalf("expected mixed preservation, got %+v", result)
	}
}

func TestRedactDoesNotMutateOriginalConfig(t *testing.T) {
	cfg := Config{PrivateKey: "secret-private", LicenseKey: "secret-license"}
	_ = Redact(cfg)
	if cfg.PrivateKey != "secret-private" || cfg.LicenseKey != "secret-license" {
		t.Fatalf("original config mutated = %+v", cfg)
	}
}

func TestSetDefaultsPartialConfig(t *testing.T) {
	cfg := Config{Endpoint: "ep.example:1111", SocksPort: 2222}
	SetDefaults(&cfg)
	if cfg.Endpoint != "ep.example:1111" {
		t.Errorf("Endpoint overwritten, got %q", cfg.Endpoint)
	}
	if cfg.SocksPort != 2222 {
		t.Errorf("SocksPort overwritten, got %d", cfg.SocksPort)
	}
	if cfg.SocksListen != "127.0.0.1" {
		t.Errorf("SocksListen default wrong, got %q", cfg.SocksListen)
	}
	if cfg.MTU != 1280 {
		t.Errorf("MTU default wrong, got %d", cfg.MTU)
	}
}

func TestRedactedMarkerValue(t *testing.T) {
	cfg := Config{PrivateKey: strings.Repeat("x", 64), LicenseKey: "license"}
	redacted := Redact(cfg)
	if redacted.PrivateKey != "[REDACTED]" {
		t.Errorf("expected [REDACTED], got %q", redacted.PrivateKey)
	}
	if redacted.LicenseKey != "[REDACTED]" {
		t.Errorf("expected [REDACTED], got %q", redacted.LicenseKey)
	}
}
