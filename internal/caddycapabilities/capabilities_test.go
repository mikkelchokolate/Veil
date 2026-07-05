package caddycapabilities

import "testing"

func TestProbeParsesModuleList(t *testing.T) {
	// A mock binary that prints a module list matching Caddy's `caddy list-modules --json` shape.
	// For the plan we test parsing of a known JSON fragment.
	input := `[
	  {"module_name":"http.handlers.forward_proxy"},
	  {"module_name":"http"}
	]`
	caps, err := parseModuleList([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if !caps.ForwardProxy {
		t.Error("expected ForwardProxy=true")
	}
}
