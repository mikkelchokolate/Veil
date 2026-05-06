package api

import "testing"

func TestWarpManagementPreservesRedactedSecretsDefaultsAndSaves(t *testing.T) {
	warp := WarpConfig{PrivateKey: "private-secret", LicenseKey: "license-secret"}
	saves := 0
	management := NewWarpManagement(&warp, func() error {
		saves++
		return nil
	})

	updated, err := management.Update(WarpConfig{Enabled: true, PrivateKey: "[REDACTED]", LicenseKey: "[REDACTED]"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if warp.PrivateKey != "private-secret" || warp.LicenseKey != "license-secret" {
		t.Fatalf("stored WARP secrets not preserved: %+v", warp)
	}
	if warp.Endpoint != "engage.cloudflareclient.com:2408" || warp.SocksListen != "127.0.0.1" || warp.SocksPort != 40000 || warp.MTU != 1280 {
		t.Fatalf("defaults not applied: %+v", warp)
	}
	if updated.PrivateKey != "[REDACTED]" || updated.LicenseKey != "[REDACTED]" {
		t.Fatalf("response should redact WARP secrets: %+v", updated)
	}
	if saves != 1 {
		t.Fatalf("saves = %d, want 1", saves)
	}
}
