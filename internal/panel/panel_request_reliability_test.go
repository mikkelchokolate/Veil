package panel

import (
	"strings"
	"testing"
)

func TestPanelRequestHelperClonesOptionsAndHeaders(t *testing.T) {
	js := panelRequestReliabilityJS()
	for _, want := range []string{
		`const requestOptions = Object.assign({}, options || {});`,
		`requestHeaders(Object.assign({}, requestOptions.headers || {}))`,
		`return true;`,
		`return null;`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("request helper missing %q", want)
		}
	}
	for _, unsafe := range []string{
		`const requestOptions = options || {};`,
		`requestOptions.headers = requestHeaders(requestOptions.headers || {});`,
	} {
		if strings.Contains(js, unsafe) {
			t.Fatalf("request helper must not mutate caller-owned options through %q", unsafe)
		}
	}
}
