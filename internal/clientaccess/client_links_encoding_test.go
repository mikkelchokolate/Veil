package clientaccess

import (
	"net/url"
	"testing"
)

// TestHysteria2ClientURIEncodesUserinfoPerRFC3986 locks in audit #71/#120: the
// password-only userinfo must be percent-encoded (space -> %20), not
// query-encoded (space -> '+'), because hysteria clients percent-decode
// userinfo without translating '+' back to a space. A password with a space
// previously produced hysteria2://my+secret+pass@... and authentication
// failed silently. (In the password-only form the client treats the whole
// userinfo as the password; Go's url.Parse maps it to Username().)
func TestHysteria2ClientURIEncodesUserinfoPerRFC3986(t *testing.T) {
	uri := Hysteria2ClientURI("example.com", 443, "my secret pass", "veil", false)
	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse %q: %v", uri, err)
	}
	if parsed.User == nil {
		t.Fatalf("expected userinfo in %q", uri)
	}
	if got := parsed.User.Username(); got != "my secret pass" {
		t.Fatalf("round-trip password = %q, want %q (uri: %s)", got, "my secret pass", uri)
	}
	if containsPlus := hasPlus(parsed.User.String()); containsPlus {
		t.Fatalf("userinfo %q contains literal '+', which hysteria clients will not decode back to a space", parsed.User.String())
	}
}

// TestHysteria2ClientURIEncodesReservedChars covers @, :, /, ? and # in the
// password: all must survive a parse round-trip.
func TestHysteria2ClientURIEncodesReservedChars(t *testing.T) {
	for _, password := range []string{"p@ss", "p:ss", "p/ss", "p?ss", "p#ss", "p&ss", "p+ss", "пароль"} {
		uri := Hysteria2ClientURI("example.com", 443, password, "veil", false)
		parsed, err := url.Parse(uri)
		if err != nil {
			t.Fatalf("parse %q (password %q): %v", uri, password, err)
		}
		got := parsed.User.Username()
		if got != password {
			t.Fatalf("password %q round-tripped as %q via %q", password, got, uri)
		}
	}
}

// TestEscapeUserInfoComponent ensures the helper percent-encodes without
// touching unreserved characters.
func TestEscapeUserInfoComponent(t *testing.T) {
	if got := escapeUserInfoComponent("abc-_.~123"); got != "abc-_.~123" {
		t.Fatalf("unreserved chars must stay: %q", got)
	}
	if got := escapeUserInfoComponent("a b"); got != "a%20b" {
		t.Fatalf("space must become %%20, got %q", got)
	}
	if got := escapeUserInfoComponent("p@ss"); got != "p%40ss" {
		t.Fatalf("@ must become %%40, got %q", got)
	}
}

func hasPlus(userinfo string) bool {
	for _, r := range userinfo {
		if r == '+' {
			return true
		}
	}
	return false
}
