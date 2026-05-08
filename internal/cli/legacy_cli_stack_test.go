package cli

import (
	"os"
	"strings"
	"testing"
)

func TestLegacyCLIStackCompatibilityIsLocalized(t *testing.T) {
	for _, path := range []string{"install.go", "repair.go"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(body)
		for _, unwanted := range []string{"legacyStack", "deprecatedPort", "deprecatedDomain", "deprecatedEmail", `MarkHidden("stack")`, `StringVar(&legacy`} {
			if strings.Contains(text, unwanted) {
				t.Fatalf("%s should not carry legacy CLI compatibility detail %q; keep that Implementation in LegacyCLICompatibility", path, unwanted)
			}
		}
	}
}
