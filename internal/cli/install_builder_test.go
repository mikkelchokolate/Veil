package cli

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

func TestBuildRURecommendedInstallFromOptionsHonorsExplicitPanelPort(t *testing.T) {
	install, err := buildRURecommendedInstallFromOptions(ruRecommendedInstallOptions{
		Domain:    "example.com",
		Email:     "admin@example.com",
		PanelPort: 2096,
	})
	if err != nil {
		t.Fatalf("buildRURecommendedInstallFromOptions: %v", err)
	}
	if install.Profile.Domain != "example.com" || install.Profile.Email != "admin@example.com" {
		t.Fatalf("unexpected profile: %+v", install.Profile)
	}
	if install.PanelPort != 2096 || install.PanelRandom {
		t.Fatalf("expected explicit panel port, got port=%d random=%v", install.PanelPort, install.PanelRandom)
	}
}

// #1309: `veil install --panel-port <reserved>` must fail before anything is
// written instead of persisting a panelListen that state validation rejects.
func TestBuildRURecommendedInstallFromOptionsRejectsReservedPanelPort(t *testing.T) {
	for _, port := range runtimeports.ReservedInternalTCPPorts() {
		for _, access := range []string{"local", "direct"} {
			_, err := buildRURecommendedInstallFromOptions(ruRecommendedInstallOptions{
				Domain:      "example.com",
				Email:       "admin@example.com",
				PanelAccess: access,
				PanelPort:   port,
			})
			if err == nil || !strings.Contains(err.Error(), "reserved") {
				t.Fatalf("--panel-access %s --panel-port %d: expected reserved-port error, got %v", access, port, err)
			}
		}
	}
}
