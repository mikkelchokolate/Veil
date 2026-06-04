package install

import (
	"bytes"
	"strings"
	"testing"
)

func TestInstallPromptCollectsMissingDomainEmailForPanelCaddy(t *testing.T) {
	in := strings.NewReader("random\nbad domain\nexample.com\nadmin@example.com\n")
	var out bytes.Buffer
	opts := PromptOptions{PanelAccess: "caddy", PanelAccessSet: true, PanelPort: 2096}

	if err := NewPrompt(in, &out).PromptMissingOptions(&opts); err != nil {
		t.Fatalf("PromptMissingOptions: %v", err)
	}
	if opts.PanelAccess != "caddy" || opts.Domain != "example.com" || opts.Email != "admin@example.com" || opts.PanelPort != 0 {
		t.Fatalf("opts=%+v", opts)
	}
	if !strings.Contains(out.String(), "Domain must be a valid domain name") {
		t.Fatalf("expected validation feedback, got:\n%s", out.String())
	}
	portPrompt := strings.Index(out.String(), "Panel port mode")
	domainPrompt := strings.Index(out.String(), "Domain for Veil/ACME")
	if portPrompt == -1 || domainPrompt == -1 || portPrompt > domainPrompt {
		t.Fatalf("expected port mode prompt before caddy details, got:\n%s", out.String())
	}
}

func TestInstallPromptCollectsPanelAccessAndRandomPanelPort(t *testing.T) {
	in := strings.NewReader("local\nrandom\n")
	var out bytes.Buffer
	opts := PromptOptions{PanelPort: 2096}

	if err := NewPrompt(in, &out).PromptMissingOptions(&opts); err != nil {
		t.Fatalf("PromptMissingOptions: %v", err)
	}
	if opts.PanelAccess != "local" || opts.PanelPort != 0 {
		t.Fatalf("opts=%+v", opts)
	}
	for _, want := range []string{"Panel access mode", "Panel port mode"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected prompt %q, got:\n%s", want, out.String())
		}
	}
}

func TestInstallPromptCollectsCustomPanelPort(t *testing.T) {
	in := strings.NewReader("direct\ncustom\n3096\n")
	var out bytes.Buffer
	opts := PromptOptions{PanelPort: 2096}

	if err := NewPrompt(in, &out).PromptMissingOptions(&opts); err != nil {
		t.Fatalf("PromptMissingOptions: %v", err)
	}
	if opts.PanelAccess != "direct" || opts.PanelPort != 3096 {
		t.Fatalf("opts=%+v", opts)
	}
}

func TestInstallPromptKeepsExplicitRandomPanelPort(t *testing.T) {
	in := strings.NewReader("local\n")
	var out bytes.Buffer
	opts := PromptOptions{PanelPort: 0, PanelPortSet: true}

	if err := NewPrompt(in, &out).PromptMissingOptions(&opts); err != nil {
		t.Fatalf("PromptMissingOptions: %v", err)
	}
	if opts.PanelAccess != "local" || opts.PanelPort != 0 {
		t.Fatalf("opts=%+v", opts)
	}
	if strings.Contains(out.String(), "Panel port mode") {
		t.Fatalf("explicit --panel-port 0 should not ask for port mode:\n%s", out.String())
	}
}
