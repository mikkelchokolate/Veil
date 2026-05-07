package cli

import "testing"

func TestPanelAccessModeBuildsListenAddressAndCaddyRequirement(t *testing.T) {
	for _, tc := range []struct {
		mode       string
		port       int
		wantListen string
		wantCaddy  bool
	}{
		{"direct", 2096, "0.0.0.0:2096", false},
		{"local", 2096, "127.0.0.1:2096", false},
		{"caddy", 2096, "127.0.0.1:2096", true},
		{"", 2096, "127.0.0.1:2096", false},
	} {
		mode, err := NewPanelAccessMode(tc.mode).Resolve(tc.port)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", tc.mode, err)
		}
		if mode.PanelListen != tc.wantListen || mode.RequiresCaddy != tc.wantCaddy {
			t.Fatalf("Resolve(%q) = %+v", tc.mode, mode)
		}
	}
}

func TestPanelAccessModeRejectsUnknownMode(t *testing.T) {
	_, err := NewPanelAccessMode("public").Resolve(2096)
	if err == nil || err.Error() != "panel access must be direct, local, or caddy" {
		t.Fatalf("err = %v", err)
	}
}
