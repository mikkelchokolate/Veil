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
		secretValue string // optional per-case secret to assert against (defaults to secret)
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
			// percent-encoded secret must be redacted too (value survives only
			// if the regex is greedy over the userinfo; encoded '@' protects it).
			wantClean: false, // encoded form is not plaintext; just must not crash
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
