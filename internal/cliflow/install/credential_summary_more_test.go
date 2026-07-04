package install

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/installer"
)

func TestCredentialSummaryMore(t *testing.T) {
	tests := []struct {
		name    string
		profile installer.RURecommendedProfile
		want    []string
	}{
		{
			name: "preserves existing password when empty",
			profile: installer.RURecommendedProfile{
				Username: "admin",
				Password: "",
			},
			want: []string{
				"Username: admin",
				"Password: [preserved existing password]",
			},
		},
		{
			name: "shows password when present",
			profile: installer.RURecommendedProfile{
				Username: "admin",
				Password: "secret123",
			},
			want: []string{
				"Username: admin",
				"Password: secret123",
			},
		},
		{
			name: "panel url with domain and empty base path",
			profile: installer.RURecommendedProfile{
				Domain:   "panel.example.com",
				Username: "admin",
				Password: "secret",
			},
			want: []string{
				"Panel: https://panel.example.com/",
				"Password: secret",
			},
		},
		{
			name: "panel url direct http without tls",
			profile: installer.RURecommendedProfile{
				PanelListen:     "127.0.0.1:8080",
				PanelTLSEnabled: false,
				WebBasePath:     "/base/",
				Username:        "admin",
			},
			want: []string{
				"Panel: http://127.0.0.1:8080/base/",
			},
		},
		{
			name: "panel url fallback when domain and listen empty",
			profile: installer.RURecommendedProfile{
				WebBasePath: "/fallback/",
				Username:    "admin",
			},
			want: []string{
				"Panel: https:///fallback/",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CredentialSummary(tt.profile)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Fatalf("summary missing %q:\n%s", w, got)
				}
			}
		})
	}
}
