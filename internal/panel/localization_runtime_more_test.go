package panel

import (
	"errors"
	"testing"
)

func TestLocalizationRuntimeJSEmptyOnMarshalError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) {
		return nil, errors.New("simulated marshal failure")
	}
	defer func() { jsonMarshal = orig }()

	if got := LocalizationRuntimeJS(); got != "" {
		t.Fatalf("LocalizationRuntimeJS should return empty string on marshal error, got %q", got)
	}
}

func TestLocalizationRuntimeJSUsesMarshalHook(t *testing.T) {
	orig := jsonMarshal
	called := false
	jsonMarshal = func(v any) ([]byte, error) {
		called = true
		return orig(v)
	}
	defer func() { jsonMarshal = orig }()

	_ = LocalizationRuntimeJS()
	if !called {
		t.Fatal("LocalizationRuntimeJS did not use jsonMarshal hook")
	}
}
