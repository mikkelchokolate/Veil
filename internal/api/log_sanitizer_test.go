package api

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/settings"
)

// TestSanitizeServiceLogOutputSecretFormats locks in audit #145: the log
// sanitizer must redact every protocol secret format Veil actually renders.
// Each case carries the exact shape produced by the real renderers/links.
func TestSanitizeServiceLogOutputSecretFormats(t *testing.T) {
	const secret = "SUPERSECRETVALUE123"
	redacted := settings.RedactedSecret

	cases := []struct {
		name        string
		in          string
		wantClean   bool
		secretValue string   // optional per-case secret to assert against (defaults to secret)
		wantPresent []string // optional substrings that must survive redaction
	}{
		{
			name: "hysteria2 password-only userinfo",
			in:   "client connected to hysteria2://" + secret + "@example.com:443/?insecure=1#veil",
			// No colon in userinfo: old logUserInfoPattern (user:pass@) missed it.
			wantClean: true,
		},
		{
			name:      "hysteria2 user:pass userinfo",
			in:        "connecting hysteria2://alice:" + secret + "@example.com:443/",
			wantClean: true,
		},
		{
			name: "olcrtc fragment key",
			// olcrtc encryption keys are 64 lowercase hex chars (renderer
			// autogenerates); the secret lives in the URI fragment.
			in:        "olcrtc://telemost?datachannel@room#" + strings.Repeat("a", 64) + "$mimo",
			wantClean: true,
			// The fragment value is not the shared test secret; assert the
			// specific hex key is gone instead.
			secretValue: strings.Repeat("a", 64),
		},
		{
			name:      "olcrtc YAML crypto key",
			in:        "crypto:\n  key: " + secret + "\n  cipher: aes256",
			wantClean: true,
		},
		{
			name:      "caddy JSON auth_credentials",
			in:        `{"handler":"forward_proxy","auth_credentials":["` + secret + `"],"hide_ip":true}`,
			wantClean: true,
		},
		{
			name:        "caddy JSON auth_credentials multiple entries",
			in:          `{"auth_credentials":["` + secret + `","SECONDSECRETVALUE456"]}`,
			wantClean:   true,
			secretValue: "SECONDSECRETVALUE456",
		},
		{
			name: "hysteria2 YAML password value",
			in:   "auth:\n  type: password\n  password: " + secret,
			// Old logSecretPattern matched the bare "password" inside
			// "type: password", swallowed the newline and kept the real value.
			wantClean: true,
		},
		{
			name:      "bearer token",
			in:        "Authorization: Bearer " + secret,
			wantClean: true,
		},
		{
			// audit: the generic authorization matcher only consumed the
			// "Basic" scheme token and left the credential visible.
			name:      "authorization basic header",
			in:        "Authorization: Basic " + secret,
			wantClean: true,
		},
		{
			name:      "proxy-authorization basic header",
			in:        "Proxy-Authorization: Basic " + secret,
			wantClean: true,
		},
		{
			// journalctl -o short-iso prefixes renderer YAML lines; the
			// olcRTC crypto "key:" secret must still be redacted (same
			// tolerance the userpass block already had).
			name: "journalctl-prefixed olcrtc YAML crypto key",
			in: "2026-08-13T10:00:00Z host olcrtc[1]: crypto:\n" +
				"2026-08-13T10:00:00Z host olcrtc[1]:   key: " + secret + "\n" +
				"2026-08-13T10:00:00Z host olcrtc[1]:   cipher: aes256",
			wantClean: true,
		},
		{
			name:      "journalctl-prefixed YAML password",
			in:        "2026-08-13T10:00:00Z host hysteria[1]:   password: " + secret,
			wantClean: true,
		},
		{
			// audit #186: snake_case JSON key used by Caddy
			name:      "caddy JSON auth_pass snake_case",
			in:        `{"auth_pass":"` + secret + `","username":"alice"}`,
			wantClean: true,
		},
		{
			// audit #186: hysteria2 userpass map with arbitrary usernames;
			// EVERY entry must be redacted, not just the first
			// (code-review round 3 P1).
			name:      "hysteria2 userpass map multiple entries",
			in:        "auth:\n  type: userpass\n  userpass:\n    alice: " + secret + "\n    bob: " + secret + "2\n    carol: " + secret + "3",
			wantClean: true,
		},
		{
			name: "journalctl-prefixed hysteria2 userpass",
			in: "2026-08-13T10:00:00Z host hysteria[1]:   userpass:\n" +
				"2026-08-13T10:00:00Z host hysteria[1]:     alice: " + secret + "\n" +
				"2026-08-13T10:00:00Z host hysteria[1]:     bob: " + secret + "2",
			wantClean: true,
		},
		{
			name:        "public subscription path",
			in:          "GET /s/" + strings.Repeat("A", 43) + " HTTP/1.1",
			wantClean:   true,
			secretValue: strings.Repeat("A", 43),
		},
		{
			name:        "public subscription URL",
			in:          "https://panel.example.com/s/" + strings.Repeat("A", 43),
			wantClean:   true,
			secretValue: strings.Repeat("A", 43),
		},
		{
			// audit #186: Caddyfile basic_auth directive
			name:      "caddy basic_auth directive",
			in:        "basic_auth alice " + secret + " {\n  realm vpn\n}",
			wantClean: true,
		},
		{
			name:      "generic secret=value",
			in:        "config: secret=" + secret,
			wantClean: true,
		},
		{
			name: "query-escaped password in query string",
			in:   "hysteria2://alice:p%40ss@example.com:443/?insecure=1#veil",
			// Percent-encoding is not a redaction: p%40ss decodes to the real
			// password, so the escaped form must be redacted too.
			wantClean:   true,
			secretValue: "p%40ss",
		},
		{
			// #1091: "[^"]*" stops at the first escaped quote, leaking the
			// remainder of the JSON string.
			name:        "JSON password with escaped quote",
			in:          `{"password":"ab\"` + secret + `tail"}`,
			wantClean:   true,
			secretValue: secret + `tail`,
		},
		{
			// #1091: same escape blind spot for backslash and \n escapes.
			name:        "JSON token with escapes",
			in:          `{"token":"a\\b` + secret + `\n"}`,
			wantClean:   true,
			secretValue: "b" + secret,
		},
		{
			// #1091: "[^]]*" stops at a ] INSIDE a quoted array element —
			// everything after it leaked.
			name:        "JSON array element containing bracket",
			in:          `{"auth_credentials":["a]` + secret + `","x` + secret + `y"]}`,
			wantClean:   true,
			secretValue: "x" + secret + "y",
		},
		{
			// #1091: YAML key match was case-sensitive; echoed config may
			// use any capitalization.
			name:      "YAML Password capitalized key",
			in:        "auth:\n  Password: " + secret,
			wantClean: true,
		},
		{
			name:      "YAML KEY uppercase key",
			in:        "crypto:\n  KEY: " + secret,
			wantClean: true,
		},
		{
			name:      "YAML Auth_Credentials mixed-case key",
			in:        "auth:\n  Auth_Credentials: " + secret,
			wantClean: true,
		},
		{
			// #1091: generic secret= terminator excluded , " ' } ; so
			// token=abc,def leaked ",def" after "abc" was redacted.
			name:        "secret value containing comma",
			in:          "token=" + secret + `,leaked`,
			wantClean:   true,
			secretValue: "leaked",
		},
		{
			// #1091: a quoted value with a space lost everything after the
			// space — the opening " was consumed by the key arm.
			name:        "quoted password value containing space",
			in:          `password="` + secret + ` tail"`,
			wantClean:   true,
			secretValue: secret + " tail",
		},
		{
			name:        "single-quoted secret value containing space",
			in:          "secret='" + secret + " tail'",
			wantClean:   true,
			secretValue: secret + " tail",
		},
		{
			// #1091: unclosed quoted value must redact through end of line.
			name:        "unterminated quoted password",
			in:          `password="` + secret + ` unclosed`,
			wantClean:   true,
			secretValue: secret + " unclosed",
		},
		{
			// #1091: userpass block required exactly 4-space entry indent;
			// canonical 2-space YAML maps leaked entirely.
			name:      "hysteria2 userpass two-space indent",
			in:        "userpass:\n  alice: " + secret + "\n  bob: " + secret + "2",
			wantClean: true,
		},
		{
			name:      "USERPASS uppercase key",
			in:        "USERPASS:\n  alice: " + secret,
			wantClean: true,
		},
		{
			// #1091: flow-style map on the key line.
			name:      "userpass flow-style map",
			in:        "userpass: {alice: " + secret + ", bob: " + secret + "2}",
			wantClean: true,
		},
		{
			// #1091: a PEM truncated upstream has no END marker — the old
			// pattern required both markers and leaked every captured line.
			name:        "unterminated PEM private key",
			in:          "-----BEGIN RSA PRIVATE KEY-----\nMII" + secret + "x\nA" + secret + "B\nnextlog: no-secret-here",
			wantClean:   true,
			secretValue: "A" + secret + "B",
		},
		{
			// #1091: the unclosed-PEM fallback must NOT eat a following
			// keyword line — the keyword's value must still be redacted.
			name:      "unterminated PEM followed by keyword secret",
			in:        "-----BEGIN PRIVATE KEY-----\nMII" + secret + "x\npassword: " + secret,
			wantClean: true,
		},
		{
			// #1091: well-formed PEM must still collapse fully.
			name:        "terminated PEM private key",
			in:          "-----BEGIN EC PRIVATE KEY-----\n" + secret + "\n-----END EC PRIVATE KEY-----\ntrailing: visible",
			wantClean:   true,
			wantPresent: []string{"trailing: visible"},
		},
		{
			// #1091: indented userpass sibling key at the same level ends the
			// map — deeper content after it must not be redacted away.
			name:        "userpass sibling ends map",
			in:          "  userpass:\n    alice: " + secret + "\n  sibling: keepme\n    deeper: keepme2",
			wantClean:   true,
			wantPresent: []string{"sibling: keepme", "deeper: keepme2"},
		},
		{
			// #1091: a keyword line following a truncated PEM keeps its key
			// visible while its value is redacted by the keyword matchers.
			name:        "unterminated PEM preserves following line",
			in:          "-----BEGIN PRIVATE KEY-----\nMII" + secret + "x\nother: keepme",
			wantClean:   true,
			wantPresent: []string{"other: keepme"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			toCheck := []string{secret}
			if tc.secretValue != "" {
				toCheck = append(toCheck, tc.secretValue)
			}
			out := sanitizeServiceLogOutput(tc.in)
			for _, checkSecret := range toCheck {
				if strings.Contains(out, checkSecret) {
					t.Fatalf("secret leaked through sanitizer:\n in:  %s\n out: %s", tc.in, out)
				}
			}
			if tc.wantClean && !strings.Contains(out, redacted) {
				t.Fatalf("expected redaction marker in output:\n in:  %s\n out: %s", tc.in, out)
			}
			for _, want := range tc.wantPresent {
				if !strings.Contains(out, want) {
					t.Fatalf("sanitizer removed non-secret content %q:\n in:  %s\n out: %s", want, tc.in, out)
				}
			}
		})
	}
}

// TestSanitizeServiceLogOutputPreservesStructure ensures the sanitizer does not
// destroy the surrounding log line (prefix/suffix survive).
func TestSanitizeServiceLogOutputPreservesStructure(t *testing.T) {
	out := sanitizeServiceLogOutput("2026-08-13T10:00:00Z hysteria2://alice:supersecret@example.com:443/ [INFO] start")
	if !strings.Contains(out, "2026-08-13T10:00:00Z") || !strings.Contains(out, "[INFO] start") {
		t.Fatalf("structure destroyed: %s", out)
	}
	if strings.Contains(out, "supersecret") {
		t.Fatalf("secret survived: %s", out)
	}
}
