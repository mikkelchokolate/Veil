package api

import (
	"os"
	"strings"
	"testing"
)

func TestConstantTimePasswordEqualDoesNotHashPasswords(t *testing.T) {
	source, err := os.ReadFile("auth_session.go")
	if err != nil {
		t.Fatal(err)
	}
	// Windows checkouts keep CRLF endings; normalize before matching
	// line-oriented patterns so the scan is checkout-agnostic.
	normalized := strings.ReplaceAll(string(source), "\r\n", "\n")
	functionStart := strings.Index(normalized, "func constantTimePasswordEqual")
	if functionStart < 0 {
		t.Fatal("constantTimePasswordEqual source not found")
	}
	functionEnd := strings.Index(normalized[functionStart:], "\n}\n")
	if functionEnd < 0 {
		t.Fatal("constantTimePasswordEqual source not found")
	}
	body := normalized[functionStart : functionStart+functionEnd]
	if strings.Contains(body, "sha256") {
		t.Fatal("constantTimePasswordEqual must not pass passwords through a fast hash")
	}
}

func TestConstantTimePasswordEqual(t *testing.T) {
	for _, tc := range []struct {
		name     string
		supplied string
		expected string
		want     bool
	}{
		{name: "equal", supplied: "correct horse battery staple", expected: "correct horse battery staple", want: true},
		{name: "different content", supplied: "correct horse battery staple", expected: "correct horse battery staplf"},
		{name: "different length", supplied: "secret", expected: "secret-longer"},
		{name: "prefix", supplied: "secret", expected: "secret-"},
		{name: "unicode equal", supplied: "пароль-安全", expected: "пароль-安全", want: true},
		{name: "empty equal", supplied: "", expected: "", want: true},
		{name: "oversize rejected", supplied: strings.Repeat("x", maxFallbackPasswordBytes+1), expected: strings.Repeat("x", maxFallbackPasswordBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := constantTimePasswordEqual(tc.supplied, tc.expected); got != tc.want {
				t.Fatalf("constantTimePasswordEqual() = %v, want %v", got, tc.want)
			}
		})
	}
}
