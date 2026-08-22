package api

import (
	"os"
	"strings"
	"testing"
)

func TestRequestAndJobExternalOperationsNeverUseBackgroundContext(t *testing.T) {
	paths := []string{
		"management_apply_context.go",
		"apply_subsystem.go",
		"backup_routes.go",
		"panel_update.go",
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "context.Background()") {
			t.Errorf("%s discards request/job/shutdown cancellation with context.Background()", path)
		}
	}
}

func TestManagementApplyExternalOperationsAreNotLockedMethods(t *testing.T) {
	body, err := os.ReadFile("management_apply_context.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	for _, name := range []string{
		"promoteStagedConfigsLocked",
		"reloadPromotedServicesLocked",
		"rollbackPromotedConfigsLocked",
		"checkServiceHealthLocked",
		"syncFirewallLocked",
	} {
		if strings.Contains(source, "func (ctx ManagementApplyContext) "+name) {
			t.Errorf("external operation %s is still modeled as executing under management mutex", name)
		}
	}
	if strings.Contains(source, "intentionally non-fatal") {
		t.Fatal("firewall failure is still explicitly excluded from apply failure/rollback")
	}
}
