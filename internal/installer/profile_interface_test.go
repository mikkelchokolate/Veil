package installer

import (
	"reflect"
	"testing"
)

func TestRURecommendedProfileInputDoesNotExposeProtocolInstallPlanning(t *testing.T) {
	inputType := reflect.TypeOf(RURecommendedInput{})
	for _, field := range []string{"Stack", "Port", "Availability", "RandomPort"} {
		if _, ok := inputType.FieldByName(field); ok {
			t.Fatalf("RURecommendedInput should not expose protocol install planning field %s", field)
		}
	}
}

func TestRURecommendedProfileDoesNotExposeSharedProxyPortPlan(t *testing.T) {
	profileType := reflect.TypeOf(RURecommendedProfile{})
	if _, ok := profileType.FieldByName("PortPlan"); ok {
		t.Fatal("RURecommendedProfile should not expose install-time shared proxy port plan")
	}
}

func TestRURecommendedInstallInputDoesNotExposeProtocolStackSelection(t *testing.T) {
	inputType := reflect.TypeOf(RURecommendedInstallInput{})
	if _, ok := inputType.FieldByName("Stack"); ok {
		t.Fatalf("RURecommendedInstallInput should not expose protocol stack selection")
	}
}
