package install

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/installer"
)

func TestPresentationMore(t *testing.T) {
	tests := []struct {
		name    string
		fn      func(Presentation)
		want    []string
		notWant []string
	}{
		{
			name: "dns check with resolved ips public ip and warnings",
			fn: func(p Presentation) {
				p.PrintDNSCheck(hostenv.DNSCheck{
					Domain:      "example.com",
					PublicIP:    "1.2.3.4",
					ResolvedIPs: []string{"1.2.3.4", "5.6.7.8"},
					Warnings:    []string{"DNS mismatch"},
				})
			},
			want: []string{
				"DNS check",
				"Domain: example.com",
				"Public IP: 1.2.3.4",
				"Resolved IPs: 1.2.3.4, 5.6.7.8",
				"Warning: DNS mismatch",
			},
		},
		{
			name: "dns check without public ip or resolved ips",
			fn: func(p Presentation) {
				p.PrintDNSCheck(hostenv.DNSCheck{Domain: "x.example.com"})
			},
			want: []string{
				"DNS check",
				"Domain: x.example.com",
				"Resolved IPs: none",
			},
			notWant: []string{"Public IP:"},
		},
		{
			name: "ru recommended apply mode without caddy",
			fn: func(p Presentation) {
				p.PrintRURecommended(installer.RURecommendedProfile{
					Domain:            "app.example.com",
					Email:             "admin@app.example.com",
					InstallPanelCaddy: false,
				}, false)
			},
			want: []string{
				"Veil ru-recommended apply",
				"Domain: app.example.com",
				"Email: admin@app.example.com",
				"Install scope: Panel",
			},
			notWant: []string{"Generated Caddyfile"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			tt.fn(NewPresentation(&out))
			got := out.String()
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Fatalf("output missing %q:\n%s", w, got)
				}
			}
			for _, nw := range tt.notWant {
				if strings.Contains(got, nw) {
					t.Fatalf("output unexpectedly contains %q:\n%s", nw, got)
				}
			}
		})
	}
}

func TestRedactProfileSecretsEmptyToken(t *testing.T) {
	input := "Panel token: panel-secret"
	got := NewPresentation(nil).RedactProfileSecrets(installer.RURecommendedProfile{}, input)
	if got != input {
		t.Fatalf("expected unchanged text, got:\n%s", got)
	}
}
