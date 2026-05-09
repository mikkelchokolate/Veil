package observability

import "testing"

func TestPrometheusLabelEscapesSpecialCharacters(t *testing.T) {
	got := PrometheusLabelValue("/api/\"x\"\\line\nnext")
	want := "/api/\\\"x\\\"\\\\line\\nnext"
	if got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}
