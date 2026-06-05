package panel

import (
	"net/http"
	"strconv"
	"strings"
)

const (
	LocaleEnglish = "en"
	LocaleRussian = "ru"
)

func NormalizeLocale(value string) string {
	if locale, ok := ParseLocale(value); ok {
		return locale
	}
	return LocaleEnglish
}

func ParseLocale(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case value == LocaleEnglish || strings.HasPrefix(value, LocaleEnglish+"-"):
		return LocaleEnglish, true
	case value == LocaleRussian || strings.HasPrefix(value, LocaleRussian+"-"):
		return LocaleRussian, true
	default:
		return "", false
	}
}

func ResolveLocale(preferred string, request *http.Request) string {
	if strings.TrimSpace(preferred) != "" {
		return NormalizeLocale(preferred)
	}
	if request == nil {
		return LocaleEnglish
	}
	if cookie, err := request.Cookie("veil_locale"); err == nil {
		if locale, ok := ParseLocale(cookie.Value); ok {
			return locale
		}
	}
	return acceptLanguageLocale(request.Header.Get("Accept-Language"))
}

func acceptLanguageLocale(header string) string {
	bestLocale := LocaleEnglish
	bestQuality := -1.0
	for _, raw := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(raw), ";")
		locale, ok := ParseLocale(parts[0])
		if !ok {
			continue
		}
		quality := 1.0
		for _, parameter := range parts[1:] {
			key, value, found := strings.Cut(strings.TrimSpace(parameter), "=")
			if !found || strings.TrimSpace(key) != "q" {
				continue
			}
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err == nil {
				quality = parsed
			}
		}
		if quality > bestQuality {
			bestLocale = locale
			bestQuality = quality
		}
	}
	return bestLocale
}
