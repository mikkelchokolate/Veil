package installer

import "testing"

func TestRURecommendedInputDefaultsFillMissingAdapters(t *testing.T) {
	input := NewRURecommendedInputDefaults().Apply(RURecommendedInput{})
	if got := input.Secret("panel"); got != "panel" {
		t.Fatalf("secret = %q", got)
	}
}

func TestRURecommendedInputDefaultsPreserveProvidedAdapters(t *testing.T) {
	input := NewRURecommendedInputDefaults().Apply(RURecommendedInput{
		Secret: func(label string) string { return "secret-" + label },
	})
	if input.Secret("naive") != "secret-naive" {
		t.Fatalf("input defaults were not preserved")
	}
}
