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

// #1309: a custom panel port on a Veil-internal loopback service port is
// re-asked instead of being accepted and failing only after every prompt.
func TestInstallPromptRejectsReservedCustomPanelPort(t *testing.T) {
	in := strings.NewReader("direct\ncustom\n44080\n61001\n2019\n61000\n3096\n")
	var out bytes.Buffer
	opts := PromptOptions{}

	if err := NewPrompt(in, &out).PromptMissingOptions(&opts); err != nil {
		t.Fatalf("PromptMissingOptions: %v", err)
	}
	if opts.PanelPort != 3096 {
		t.Fatalf("expected reserved ports to be re-asked and 3096 accepted, got opts=%+v\n%s", opts, out.String())
	}
	for _, want := range []string{
		"Port 44080 is reserved for the IP-certificate HTTP-01 listener",
		"Port 61001 is reserved for the Hysteria2 auth callback",
		"Port 2019 is reserved for the Caddy admin listener",
		"Port 61000 is reserved for Hysteria2 traffic statistics",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in prompt output, got:\n%s", want, out.String())
		}
	}
	if got := strings.Count(out.String(), "Panel TCP port: "); got != 5 {
		t.Fatalf("expected 5 port prompts (4 rejected + 1 accepted), got %d:\n%s", got, out.String())
	}
}
