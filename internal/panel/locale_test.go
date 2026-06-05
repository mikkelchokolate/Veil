package panel

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeLocale(t *testing.T) {
	tests := map[string]string{
		"":        "en",
		"en":      "en",
		"en-US":   "en",
		"RU":      "ru",
		"ru-RU":   "ru",
		"de-DE":   "en",
		"unknown": "en",
	}
	for input, want := range tests {
		if got := NormalizeLocale(input); got != want {
			t.Errorf("NormalizeLocale(%q)=%q want %q", input, got, want)
		}
	}
}

func TestResolveLocalePrecedence(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "veil_locale", Value: "en"})
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	if got := ResolveLocale("ru", req); got != "ru" {
		t.Fatalf("stored preference locale=%q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "veil_locale", Value: "ru"})
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	if got := ResolveLocale("", req); got != "ru" {
		t.Fatalf("cookie locale=%q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "de;q=0.9, ru-RU;q=0.8, en;q=0.7")
	if got := ResolveLocale("", req); got != "ru" {
		t.Fatalf("accept-language locale=%q", got)
	}
}
