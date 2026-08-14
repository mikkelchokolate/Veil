package api

import (
	"regexp"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/settings"
)

const maxServiceLogResponseBytes = 256 * 1024

var (
	logBearerPattern        = regexp.MustCompile(`(?i)(\bbearer\s+)[A-Za-z0-9._~+/=-]+`)
	logJSONSecretPattern    = regexp.MustCompile(`(?i)("(?:password|passwd|token|secret|private[_-]?key|license[_-]?key|authorization|auth_credentials|auth_pass|basic_auth)"\s*:\s*\[?\")[^"]*(")`)
	logSecretPattern        = regexp.MustCompile(`(?i)(\b(?:password|passwd|token|secret|private[_-]?key|license[_-]?key|authorization|auth_pass)\b["']?[ 	]*(?::|=|[ 	])[ 	]*["']?)[^ 	,\"'};]+`)
	logUserInfoPattern      = regexp.MustCompile(`(://[^:/\s]+:)[^@/\s]+(@)`)
	logUserInfoOnlyPattern  = regexp.MustCompile(`(://)[^@/:\s]+(@)`)
	logFragmentKeyPattern   = regexp.MustCompile(`(#)[0-9a-fA-F]{32,64}(\$)`)
	logPEMPrivateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	// Caddyfile basic_auth <user> <hash>: redact the credential hash.
	logBasicAuthPattern = regexp.MustCompile(`(?i)(\bbasic_auth\s+[A-Za-z0-9_.-]+\s+)[^ 	\n{]+`)
	// Multi-line YAML secret values: redact only the value on the key's line,
	// never the rest of the document (audit #179/#186: a greedy \s* pattern
	// crossing newlines ate "type: password" and left the secret in place).
	logYAMLSecretPattern = regexp.MustCompile(`(?m)^(\s*(?:password|passwd|token|secret|private[_-]?key|license[_-]?key|auth_pass|auth_credentials|key)\s*:\s*)([^#\n][^\n]*)$`)
	// hysteria2 userpass map: "alice: SECRET" entries nested directly under a
	// "userpass:" key. The block pattern captures userpass: plus ALL following
	// entries, and each entry line is redacted inside the callback — anchoring
	// on the block prevents over-redaction of unrelated 4-space YAML keys
	// (port:, sni:, ...) while covering every map entry, not just the first
	// (code-review round 3 P1: single-line anchor leaked 2nd+ entries).
	logUserPassBlockPattern = regexp.MustCompile(`(?m)^[ 	]*userpass:[^\n]*\n(?:[ 	]{4,}[A-Za-z][A-Za-z0-9_.-]*: [^\n]*\n?)+`)
	logUserPassEntryPattern = regexp.MustCompile(`(?m)^([ 	]{4,}[A-Za-z][A-Za-z0-9_.-]*: )[^\n]+`)
)

func sanitizeServiceLogOutput(output string) string {
	output = logPEMPrivateKeyPattern.ReplaceAllString(output, settings.RedactedSecret)
	output = logBearerPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret)
	output = logJSONSecretPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret+`${2}`)
	// basic_auth must run before logSecretPattern, which would otherwise
	// redact the username ("basic_auth alice <hash>") and leave the hash.
	output = logBasicAuthPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret)
	output = logSecretPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret)
	output = logYAMLSecretPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret)
	output = logUserPassBlockPattern.ReplaceAllStringFunc(output, func(block string) string {
		return logUserPassEntryPattern.ReplaceAllString(block, `${1}`+settings.RedactedSecret)
	})
	output = logUserInfoPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret+`${2}`)
	output = logUserInfoOnlyPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret+`${2}`)
	output = logFragmentKeyPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret+`${2}`)
	if len(output) <= maxServiceLogResponseBytes {
		return output
	}
	const marker = "\n...[TRUNCATED]"
	prefix := strings.ToValidUTF8(output[:maxServiceLogResponseBytes-len(marker)], "")
	return strings.TrimRight(prefix, "\x00") + marker
}
