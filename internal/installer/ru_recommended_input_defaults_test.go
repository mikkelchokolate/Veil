package installer

import "testing"

func TestRURecommendedInputDefaultsFillMissingAdapters(t *testing.T) {
	input := NewRURecommendedInputDefaults().Apply(RURecommendedInput{})
	if got := input.Secret("panel"); got != "panel" {
		t.Fatalf("secret = %q", got)
	}
	if got := input.RandomPort(); got != 443 {
		t.Fatalf("random port = %d", got)
	}
}

func TestRURecommendedInputDefaultsPreserveProvidedAdapters(t *testing.T) {
	input := NewRURecommendedInputDefaults().Apply(RURecommendedInput{
		Secret:     func(label string) string { return "secret-" + label },
		RandomPort: func() int { return 31874 },
	})
	if input.Secret("naive") != "secret-naive" || input.RandomPort() != 31874 {
		t.Fatalf("input defaults were not preserved")
	}
}
