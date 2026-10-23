package cli

import (
	"testing"

	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
)

func TestMetricsAccessBanner(t *testing.T) {
	cases := []struct {
		cfg  serveflow.Config
		want string
	}{
		{serveflow.Config{MetricsAuthRequired: true, MetricsAuthWhenUsersConfigured: true, MetricsAccessSource: "default"}, "Metrics access: authenticated (default)"},
		{serveflow.Config{MetricsAuthWhenUsersConfigured: true, MetricsAccessSource: "default"}, "Metrics access: public until a Panel user exists (default)"},
		{serveflow.Config{MetricsAccessSource: "--metrics-access"}, "Metrics access: public (--metrics-access)"},
	}
	for _, tc := range cases {
		if got := metricsAccessBanner(tc.cfg); got != tc.want {
			t.Errorf("metricsAccessBanner(%+v) = %q, want %q", tc.cfg, got, tc.want)
		}
	}
}
