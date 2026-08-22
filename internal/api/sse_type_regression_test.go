package api

import "testing"

func TestSSETypesAreExactCommaSeparatedTokens(t *testing.T) {
	got := parseSSETypes(" traffic,apply,traffic-extra,preapply,, TRAFFIC ")
	if !got["traffic"] || !got["apply"] || len(got) != 2 {
		t.Fatalf("types=%v", got)
	}
}
