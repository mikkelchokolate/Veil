package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestInstallPromptCollectsMissingDomainEmailForPanelCaddy(t *testing.T) {
	in := strings.NewReader("bad domain\nexample.com\nadmin@example.com\nn\n")
	var out bytes.Buffer
	domain := ""
	email := ""
	sharedPort := 0
	panelPort := 0

	if err := NewInstallPrompt(in, &out).PromptMissingOptions("caddy", &domain, &email, &sharedPort, &panelPort); err != nil {
		t.Fatalf("PromptMissingOptions: %v", err)
	}
	if domain != "example.com" || email != "admin@example.com" || sharedPort != 0 || panelPort != 0 {
		t.Fatalf("domain=%q email=%q shared=%d panel=%d", domain, email, sharedPort, panelPort)
	}
	if !strings.Contains(out.String(), "Domain must be a valid domain name") {
		t.Fatalf("expected validation feedback, got:\n%s", out.String())
	}
}
