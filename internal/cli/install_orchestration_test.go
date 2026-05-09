package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

func TestRURecommendedInstallWorkflowOrchestratesDryRunWithoutApply(t *testing.T) {
	oldApply := installApplyFunc
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		t.Fatalf("dry-run must not apply files")
		return installer.ApplyResult{}, nil
	}
	t.Cleanup(func() { installApplyFunc = oldApply })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := NewRURecommendedInstallWorkflow(cmd, ruRecommendedInstallOptions{Profile: "ru-recommended", PanelAccess: "local", PanelPort: 2096, DryRun: true, EtcDir: "/etc/veil", VarDir: "/var/lib/veil"}).Run()
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	for _, want := range []string{"Veil ru-recommended dry run", "Install scope: Panel", "Panel port: 2096 (default)", "Install plan"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}
