package install

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/installer"
)

func TestPanelSummaryMore(t *testing.T) {
	tests := []struct {
		name  string
		input PanelSummaryInput
		want  []string
	}{
		{
			name: "random port source",
			input: PanelSummaryInput{
				Profile:     installer.RURecommendedProfile{Domain: "d.example.com", WebBasePath: "/s/"},
				PanelPort:   1234,
				PanelRandom: true,
			},
			want: []string{
				"Panel port: 1234 (random)",
				"Panel URL: https://d.example.com/s/",
			},
		},
		{
			name: "default port with empty listen and empty base path uses http",
			input: PanelSummaryInput{
				Profile:   installer.RURecommendedProfile{PanelTLSEnabled: false},
				PanelPort: 2096,
			},
			want: []string{
				"Panel port: 2096 (default)",
				"Panel access: http://127.0.0.1:2096/",
			},
		},
		{
			name: "http direct access with custom listen and base path",
			input: PanelSummaryInput{
				Profile: installer.RURecommendedProfile{
					PanelListen:     "0.0.0.0:8080",
					PanelTLSEnabled: false,
					WebBasePath:     "/base/",
				},
				PanelPort:    8080,
				PanelPortSet: true,
			},
			want: []string{
				"Panel port: 8080 (user selected)",
				"Panel access: http://0.0.0.0:8080/base/",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewPanelSummary(tt.input).String()
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Fatalf("summary missing %q:\n%s", w, got)
				}
			}
		})
	}
}
