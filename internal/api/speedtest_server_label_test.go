package api

import "testing"

func TestSpeedtestServerLabelCombinesAndTrimsProviderAndServer(t *testing.T) {
	cases := []struct {
		provider string
		server   string
		want     string
	}{
		{" ISP ", " Node ", "ISP - Node"},
		{"ISP", "", "ISP"},
		{"", "Node", "Node"},
		{" ", " ", ""},
	}
	for _, tc := range cases {
		if got := NewSpeedtestServerLabel(tc.provider, tc.server).String(); got != tc.want {
			t.Fatalf("label(%q,%q) = %q, want %q", tc.provider, tc.server, got, tc.want)
		}
	}
}
