package settings

import "testing"

func TestNormalizeWebBasePathFailsClosedForUnsafeLegacyInput(t *testing.T) {
	for _, value := range []string{
		"panel?debug",
		"panel'</script>",
		"panel\nrespond hacked",
		"panel/../admin",
	} {
		if got := NormalizeWebBasePath(value); got != "" {
			t.Fatalf("NormalizeWebBasePath(%q) = %q, want empty fail-closed value", value, got)
		}
	}
}

func TestNormalizeWebBasePathCanonicalizesValidLegacyInput(t *testing.T) {
	if got := NormalizeWebBasePath("panel/admin"); got != "/panel/admin/" {
		t.Fatalf("NormalizeWebBasePath = %q, want /panel/admin/", got)
	}
	if got := NormalizeWebBasePath("/"); got != "" {
		t.Fatalf("NormalizeWebBasePath root = %q, want empty settings representation", got)
	}
}
