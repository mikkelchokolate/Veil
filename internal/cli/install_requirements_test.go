package cli

import (
	"strings"
	"testing"
)

func TestRURecommendedInstallRequirementsAllowPanelAndMieruWithoutDomainOrPort(t *testing.T) {
	for _, stack := range []string{"panel", "mieru"} {
		if err := NewRURecommendedInstallRequirements(stack).Validate(ruRecommendedInstallOptions{Stack: stack}); err != nil {
			t.Fatalf("stack %q should not require domain/email/port: %v", stack, err)
		}
	}
}

func TestRURecommendedInstallRequirementsRequireDomainEmailAndPortForProxyStacks(t *testing.T) {
	err := NewRURecommendedInstallRequirements("both").Validate(ruRecommendedInstallOptions{Stack: "both"})
	if err == nil || !strings.Contains(err.Error(), "--domain is required") {
		t.Fatalf("missing domain err = %v", err)
	}
	err = NewRURecommendedInstallRequirements("both").Validate(ruRecommendedInstallOptions{Stack: "both", Domain: "example.com", Email: "admin@example.com", SharedPort: 443})
	if err != nil {
		t.Fatalf("valid proxy requirements: %v", err)
	}
}
