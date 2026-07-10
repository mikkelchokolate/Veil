package panel

import (
	"strings"
	"testing"
)

func TestProtocolSchemaLoaderRecoversAfterTransientFailure(t *testing.T) {
	js := panelDynamicFieldsJS()
	for _, want := range []string{
		`window.protocolSchemaPromise = null;`,
		`.catch((err) => {`,
		`throw err;`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("dynamic protocol schema loader missing %q", want)
		}
	}

	if strings.Count(js, `window.protocolSchemaPromise = null;`) < 2 {
		t.Fatal("schema promise must be cleared after both success and failure")
	}
}
