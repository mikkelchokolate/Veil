package api

import (
	"regexp"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/settings"
)

const maxServiceLogResponseBytes = 256 * 1024

// journalctl -o short-iso prefixes every line with TIMESTAMP HOST IDENT:
// so renderer YAML that is start-of-line is mid-line in GET /api/logs.
const logJournalctlPrefix = `(?:\d{4}-\d{2}-\d{2}T[^\s]+\s+\S+\s+\S+:)?`

const logJSONSecretKey = `(?:password|passwd|token|secret|private[_-]?key|license[_-]?key|authorization|auth_credentials|auth_pass|basic_auth)`

var (
	logBearerPattern           = regexp.MustCompile(`(?i)(\bbearer\s+)[A-Za-z0-9._~+/=-]+`)
	logJSONSecretPattern       = regexp.MustCompile(`(?i)("` + logJSONSecretKey + `"\s*:\s*")[^"]*(")`)
	logJSONSecretArrayPattern  = regexp.MustCompile(`(?i)("` + logJSONSecretKey + `"\s*:\s*\[)([^]]*)(\])`)
	logJSONQuotedStringPattern = regexp.MustCompile(`"[^"]*"`)
	logSecretPattern           = regexp.MustCompile(`(?i)(\b(?:password|passwd|token|secret|private[_-]?key|license[_-]?key|authorization|auth_pass)\b["']?[ 	]*(?::|=|[ 	])[ 	]*["']?)[^ 	,\"'};]+`)
	logUserInfoPattern         = regexp.MustCompile(`(://[^:/\s]+:)[^@/\s]+(@)`)
	logUserInfoOnlyPattern     = regexp.MustCompile(`(://)[^@/:\s]+(@)`)
	logFragmentKeyPattern      = regexp.MustCompile(`(#)[0-9a-fA-F]{32,64}(\$)`)
	logPEMPrivateKeyPattern    = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	// Caddyfile basic_auth <user> <hash>: redact the credential hash.
	logBasicAuthPattern = regexp.MustCompile(`(?i)(\bbasic_auth\s+[A-Za-z0-9_.-]+\s+)[^ 	\n{]+`)
	// HTTP Authorization / Proxy-Authorization "Basic <credential>": redact the
	// credential after the scheme token. The generic authorization matcher in
	// logSecretPattern only consumes the word "Basic" and leaves the secret.
	logBasicAuthHeaderPattern = regexp.MustCompile(`(?i)(\b(?:proxy-)?authorization\b["']?[ 	]*(?::|=)[ 	]*["']?basic[ 	]+)[^ 	,\"'};]+`)
	// Multi-line YAML secret values: redact only the value on the key's line,
	// never the rest of the document (audit #179/#186: a greedy \s* pattern
	// crossing newlines ate "type: password" and left the secret in place).
	// The journalctl short-iso prefix is optional so GET /api/logs redacts the
	// same keys (olcRTC crypto "key:", ...) when lines carry TIMESTAMP HOST
	// IDENT: prefixes — matching the userpass block handling below.
	logYAMLSecretPattern = regexp.MustCompile(`(?m)^(` + logJournalctlPrefix + `\s*(?:password|passwd|token|secret|private[_-]?key|license[_-]?key|auth_pass|auth_credentials|key)\s*:\s*)([^#\n][^\n]*)$`)
	// hysteria2 userpass map: "alice: SECRET" entries nested directly under a
	// "userpass:" key. The block pattern captures userpass: plus ALL following
	// entries, and each entry line is redacted inside the callback — anchoring
	// on the block prevents over-redaction of unrelated 4-space YAML keys
	// (port:, sni:, ...) while covering every map entry, not just the first
	// (code-review round 3 P1: single-line anchor leaked 2nd+ entries).
	// journalctl short-iso prefixes are optional so GET /api/logs redacts the
	// same map when it is wrapped in TIMESTAMP HOST IDENT: lines.
	logUserPassBlockPattern    = regexp.MustCompile(`(?m)^` + logJournalctlPrefix + `[ 	]*userpass:[^\n]*\n(?:` + logJournalctlPrefix + `[ 	]{4,}[A-Za-z][A-Za-z0-9_.-]*: [^\n]*\n?)+`)
	logUserPassEntryPattern    = regexp.MustCompile(`(?m)^(` + logJournalctlPrefix + `[ 	]{4,}[A-Za-z][A-Za-z0-9_.-]*: )[^\n]+`)
	logSubscriptionPathPattern = regexp.MustCompile(`(/s/)[A-Za-z0-9_-]{16,}`)
)

func sanitizeServiceLogOutput(output string) string {
	output = logPEMPrivateKeyPattern.ReplaceAllString(output, settings.RedactedSecret)
	output = logBearerPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret)
	output = logJSONSecretPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret+`${2}`)
	output = logJSONSecretArrayPattern.ReplaceAllStringFunc(output, redactJSONSecretArray)
	// basic_auth must run before logSecretPattern, which would otherwise
	// redact the username ("basic_auth alice <hash>") and leave the hash.
	output = logBasicAuthPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret)
	// Basic-auth headers must run before logSecretPattern, which would
	// otherwise redact only the "Basic" scheme token and leave the
	// credential behind.
	output = logBasicAuthHeaderPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret)
	output = logSecretPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret)
	output = logYAMLSecretPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret)
	output = logUserPassBlockPattern.ReplaceAllStringFunc(output, func(block string) string {
		return logUserPassEntryPattern.ReplaceAllString(block, `${1}`+settings.RedactedSecret)
	})
	output = logUserInfoPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret+`${2}`)
	output = logUserInfoOnlyPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret+`${2}`)
	output = logFragmentKeyPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret+`${2}`)
	output = logSubscriptionPathPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret)
	if len(output) <= maxServiceLogResponseBytes {
		return output
	}
	const marker = "\n...[TRUNCATED]"
	prefix := strings.ToValidUTF8(output[:maxServiceLogResponseBytes-len(marker)], "")
	return strings.TrimRight(prefix, "\x00") + marker
}

func redactJSONSecretArray(match string) string {
	parts := logJSONSecretArrayPattern.FindStringSubmatch(match)
	if len(parts) != 4 {
		return match
	}
	return parts[1] + logJSONQuotedStringPattern.ReplaceAllString(parts[2], `"`+settings.RedactedSecret+`"`) + parts[3]
}
