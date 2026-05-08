package installer

import (
	"reflect"
	"testing"
)

func TestRURecommendedProfileInputDoesNotExposeSharedProxyPortPlanning(t *testing.T) {
	inputType := reflect.TypeOf(RURecommendedInput{})
	for _, field := range []string{"Port", "Availability", "RandomPort"} {
		if _, ok := inputType.FieldByName(field); ok {
			t.Fatalf("RURecommendedInput should not expose shared proxy port planning field %s", field)
		}
	}
}
