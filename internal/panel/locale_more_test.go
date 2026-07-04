package panel

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveLocaleNilRequestFallsBackToEnglish(t *testing.T) {
	if got := ResolveLocale("", nil); got != LocaleEnglish {
		t.Fatalf("ResolveLocale(\"\", nil)=%q, want %q", got, LocaleEnglish)
	}
	if got := ResolveLocale("invalid-locale", nil); got != LocaleEnglish {
		t.Fatalf("ResolveLocale(\"invalid-locale\", nil)=%q, want %q", got, LocaleEnglish)
	}
}

func TestResolveLocaleInvalidCookieFallsBackToAcceptLanguage(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "veil_locale", Value: "xx-XX"})
	req.Header.Set("Accept-Language", "ru-RU,en;q=0.5")
	if got := ResolveLocale("", req); got != LocaleRussian {
		t.Fatalf("invalid cookie should fall back to Accept-Language, got %q", got)
	}
}

func TestAcceptLanguageIgnoresNonQualityParameters(t *testing.T) {
	// A parameter with a key other than "q" should be ignored and the locale
	// should still be selected with the default quality of 1.0.
	if got := acceptLanguageLocale("en;level=1"); got != LocaleEnglish {
		t.Fatalf("acceptLanguageLocale(\"en;level=1\")=%q, want %q", got, LocaleEnglish)
	}
}
