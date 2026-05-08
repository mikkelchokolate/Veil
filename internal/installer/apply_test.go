package installer

import (
	"os"
	"strings"
	"testing"
)

func mustRUProfile(t *testing.T, stack Stack) RURecommendedProfile {
	t.Helper()
	if stack == StackBoth {
		return RURecommendedProfile{Domain: "example.com", Stack: StackPanel, InstallNaive: true, InstallHysteria2: true, Caddyfile: "forward_proxy", Hysteria2YAML: "listen: :443", PanelAuthToken: "secret-panel"}
	}
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain: "example.com",
		Email:  "admin@example.com",
		Secret: func(label string) string { return "secret-" + label },
	})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}
	return profile
}

func assertRepairAction(t *testing.T, plan RepairPlan, path string, reason RepairReason) {
	t.Helper()
	for _, action := range plan.Actions {
		if action.Path == path && action.Reason == reason {
			return
		}
	}
	t.Fatalf("missing repair action path=%s reason=%s in %+v", path, reason, plan.Actions)
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected %s to be absent", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(body), want) {
		t.Fatalf("file %s missing %q:\n%s", path, want, string(body))
	}
}
