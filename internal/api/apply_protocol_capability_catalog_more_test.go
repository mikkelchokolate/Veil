package api

import (
	"strings"
	"testing"
)

func TestApplyProtocolCapabilityShouldValidateRender(t *testing.T) {
	cases := []struct {
		name                    string
		validateRender          bool
		requiresRenderSettings  bool
		renderSettingsAvailable bool
		want                    bool
	}{
		{"render not required", false, false, false, false},
		{"render required and settings available", true, true, true, true},
		{"render required and settings unavailable", true, true, false, false},
		{"render required without settings requirement", true, false, false, true},
		{"render required without settings requirement and available", true, false, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cap := ApplyProtocolCapability{
				ValidateInboundRender:  tc.validateRender,
				RequiresRenderSettings: tc.requiresRenderSettings,
			}
			if got := cap.ShouldValidateRender(tc.renderSettingsAvailable); got != tc.want {
				t.Fatalf("ShouldValidateRender(%v) = %v, want %v", tc.renderSettingsAvailable, got, tc.want)
			}
		})
	}
}

func TestApplyProtocolCapabilityValidateSettingsUsesProtocolValidator(t *testing.T) {
	cap := ApplyProtocolCapability{Protocol: "naiveproxy", ValidateSettingsRequired: true}
	err := cap.ValidateSettings(Settings{})
	if err == nil {
		t.Fatal("expected naiveproxy settings validation error")
	}
	if !strings.Contains(err.Error(), "domain, email, naive username, and naive password") {
		t.Fatalf("unexpected error: %v", err)
	}

	cap = ApplyProtocolCapability{Protocol: "hysteria2", ValidateSettingsRequired: false}
	if err := cap.ValidateSettings(Settings{}); err != nil {
		t.Fatalf("unexpected settings error: %v", err)
	}
}
