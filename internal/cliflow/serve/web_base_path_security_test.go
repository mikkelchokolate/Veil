package serve

import (
	"strings"
	"testing"
)

func TestSecurityResolveCanonicalizesRootWebBasePath(t *testing.T) {
	t.Setenv("VEIL_PANEL_ACCESS", "")
	t.Setenv("VEIL_WEB_BASE_PATH", "")
	config, err := NewSecurity(SecurityOptions{
		Listen:      "127.0.0.1:2096",
		WebBasePath: "/",
	}).Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if config.WebBasePath != "/" {
		t.Fatalf("WebBasePath = %q, want root /", config.WebBasePath)
	}
}

func TestSecurityResolveRejectsUnsafeWebBasePath(t *testing.T) {
	t.Setenv("VEIL_PANEL_ACCESS", "")
	t.Setenv("VEIL_WEB_BASE_PATH", "")
	_, err := NewSecurity(SecurityOptions{
		Listen:      "127.0.0.1:2096",
		WebBasePath: "panel'</script>",
	}).Resolve()
	if err == nil || !strings.Contains(err.Error(), "web base path:") {
		t.Fatalf("expected unsafe web base path error, got %v", err)
	}
}

func TestSecurityResolveNormalizesNestedWebBasePath(t *testing.T) {
	t.Setenv("VEIL_PANEL_ACCESS", "")
	config, err := NewSecurity(SecurityOptions{
		Listen:      "127.0.0.1:2096",
		WebBasePath: "panel/admin",
	}).Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if config.WebBasePath != "/panel/admin/" {
		t.Fatalf("WebBasePath = %q, want /panel/admin/", config.WebBasePath)
	}
}
