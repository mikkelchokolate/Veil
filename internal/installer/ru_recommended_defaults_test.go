package installer

import "testing"

func TestRURecommendedDefaultsDefinesInstallDefaults(t *testing.T) {
	defaults := NewRURecommendedDefaults()
	if defaults.Username != "veil" || defaults.MasqueradeURL != "https://www.bing.com/" || defaults.FallbackRoot != "/var/lib/veil/www" {
		t.Fatalf("defaults = %+v", defaults)
	}
}
