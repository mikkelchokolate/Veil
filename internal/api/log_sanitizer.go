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
	logBearerPattern = regexp.MustCompile(`(?i)(\bbearer\s+)[A-Za-z0-9._~+/=-]+`)
	// JSON string values must consume escapes — "[^"]*" stops at the first
	// backslash-escaped quote, so {"password":"ab\"cd"} used to leak `cd"}`
	// (#1091).
	logJSONSecretPattern       = regexp.MustCompile(`(?i)("` + logJSONSecretKey + `"\s*:\s*")(?:[^"\\]|\\.)*(")`)
	logJSONSecretArrayPattern  = regexp.MustCompile(`(?i)("` + logJSONSecretKey + `"\s*:\s*\[)((?:"(?:[^"\\]|\\.)*"|[^\]])*)(\])`)
	logJSONQuotedStringPattern = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	// The value arm is quote-aware: a value opening with " or ' consumes
	// escapes and everything up to the closing quote (or end-of-line when
	// unclosed), so `password="abc def"` no longer leaks ` def"`; bare values
	// run to whitespace so `token=abc,def` no longer leaks `,def` (#1091).
	logSecretPattern       = regexp.MustCompile(`(?i)(\b(?:password|passwd|token|secret|private[_-]?key|license[_-]?key|authorization|auth_pass)\b["']?[ 	]*(?::|=|[ 	])[ 	]*)("(?:[^"\\\n]|\\.)*"?[^\s]*|'(?:[^'\\\n]|\\.)*'?[^\s]*|[^\s]+)`)
	logUserInfoPattern     = regexp.MustCompile(`(://[^:/\s]+:)[^@/\s]+(@)`)
	logUserInfoOnlyPattern = regexp.MustCompile(`(://)[^@/:\s]+(@)`)
	logFragmentKeyPattern  = regexp.MustCompile(`(#)[0-9a-fA-F]{32,64}(\$)`)
	// A PEM truncated upstream (journal line cap, rotated fragment) has no
	// END marker — the `.+` fallback hands the tail to redactPEMBlock, which
	// redacts only the pure-base64 body lines and preserves the first
	// non-base64 line verbatim so keyword redaction still sees it (#1091).
	logPEMPrivateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----(?:.*?-----END [A-Z0-9 ]*PRIVATE KEY-----|.+)`)
	// Caddyfile basic_auth <user> <hash>: redact the credential hash.
	logBasicAuthPattern = regexp.MustCompile(`(?i)(\bbasic_auth\s+[A-Za-z0-9_.-]+\s+)[^ 	\n{]+`)
	// HTTP Authorization / Proxy-Authorization "Basic <credential>": redact the
	// credential after the scheme token. The generic authorization matcher in
	// logSecretPattern only consumes the word "Basic" and leaves the secret.
	logBasicAuthHeaderPattern = regexp.MustCompile(`(?i)(\b(?:proxy-)?authorization\b["']?[ 	]*(?::|=)[ 	]*["']?basic[ 	]+)[^\s"']+`)
	// Multi-line YAML secret values: redact only the value on the key's line,
	// never the rest of the document (audit #179/#186: a greedy \s* pattern
	// crossing newlines ate "type: password" and left the secret in place).
	// The journalctl short-iso prefix is optional so GET /api/logs redacts the
	// same keys (olcRTC crypto "key:", ...) when lines carry TIMESTAMP HOST
	// IDENT: prefixes — matching the userpass block handling below. `(?i)`
	// because echoed third-party config is not always lowercase (`Password:`,
	// `KEY:`, `Auth_Credentials:` — issue #1091).
	logYAMLSecretPattern = regexp.MustCompile(`(?im)^(` + logJournalctlPrefix + `\s*(?:password|passwd|token|secret|private[_-]?key|license[_-]?key|auth_pass|auth_credentials|key)\s*:\s*)([^#\n][^\n]*)$`)
	// hysteria2 userpass map: "alice: SECRET" entries nested directly under a
	// "userpass:" key. The block pattern captures userpass: plus ALL following
	// INDENTED lines; the callback then redacts only the entries that are
	// strictly deeper than the userpass key itself (RE2 has no backreferences
	// to express that inline), so a 2-space-indent map or a sibling key at
	// the userpass level is handled correctly — the old hard-coded
	// `[ \t]{4,}` entry requirement leaked shallower maps and over-redacted
	// deeper siblings (#1091). journalctl short-iso prefixes are optional so
	// GET /api/logs redacts the same map when wrapped in TIMESTAMP HOST
	// IDENT: lines.
	logUserPassBlockPattern  = regexp.MustCompile(`(?im)^` + logJournalctlPrefix + `[ 	]*userpass:[^\n]*\n(?:` + logJournalctlPrefix + `[ 	]*[^\n]*\n?)+`)
	logUserPassEntryPattern  = regexp.MustCompile(`(?i)^(` + logJournalctlPrefix + `[ 	]*[A-Za-z][A-Za-z0-9_.-]*:[ 	]*)[^\n]*`)
	logUserPassIndentPattern = regexp.MustCompile(`^` + logJournalctlPrefix + `([ 	]*)`)
	// Flow-style map on the key line itself: `userpass: {alice: pw}` (#1091).
	logUserPassFlowPattern     = regexp.MustCompile(`(?i)(\buserpass:[ 	]*)[\{\[][^\n]*`)
	logSubscriptionPathPattern = regexp.MustCompile(`(/s/)[A-Za-z0-9_-]{16,}`)
)

func sanitizeServiceLogOutput(output string) string {
	output = logPEMPrivateKeyPattern.ReplaceAllStringFunc(output, redactPEMBlock)
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
	output = logUserPassBlockPattern.ReplaceAllStringFunc(output, redactUserPassBlock)
	output = logUserPassFlowPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret)
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

// redactUserPassBlock redacts every "user: pass" entry INSIDE a captured
// userpass block. Entries must be indented deeper than the userpass: key
// itself — a sibling key at the same (or shallower) level ends the map, and
// everything the block regex over-captured after it is preserved verbatim.
func redactUserPassBlock(block string) string {
	lines := strings.SplitAfter(block, "\n")
	if len(lines) == 0 {
		return block
	}
	headerIndent := logLineIndent(lines[0])
	var b strings.Builder
	b.WriteString(lines[0])
	inside := true
	for _, line := range lines[1:] {
		if inside {
			switch {
			case strings.TrimSpace(line) == "":
				// Blank lines are legal inside a YAML map; they do not end
				// the userpass block.
			case logLineIndent(line) <= headerIndent:
				inside = false
			}
		}
		if inside && logUserPassEntryPattern.MatchString(line) {
			b.WriteString(logUserPassEntryPattern.ReplaceAllString(line, `${1}`+settings.RedactedSecret))
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}

// logLineIndent measures a line's leading whitespace after the optional
// journalctl short-iso prefix, in characters.
func logLineIndent(line string) int {
	m := logUserPassIndentPattern.FindStringSubmatch(line)
	if m == nil {
		return 0
	}
	return len(m[1])
}

// redactPEMBlock redacts a matched PEM private-key block. A well-formed block
// collapses to the redaction marker whole; a truncated block (no END marker)
// redacts the header and every pure-base64 body line but preserves the first
// non-base64 line and everything after it — those lines belong to later log
// entries and may carry their own secrets for the other matchers (#1091).
func redactPEMBlock(match string) string {
	if strings.Contains(match, "-----END ") {
		return settings.RedactedSecret
	}
	nl := strings.IndexByte(match, '\n')
	if nl < 0 {
		return settings.RedactedSecret
	}
	head, rest := match[:nl], match[nl+1:]
	consumed := 0
	for consumed < len(rest) {
		lineEnd := strings.IndexByte(rest[consumed:], '\n')
		line := rest[consumed:]
		if lineEnd >= 0 {
			line = line[:lineEnd+1]
		}
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !isPEMBase64Line(trimmed) {
			break
		}
		consumed += len(line)
	}
	return head + "\n" + settings.RedactedSecret + rest[consumed:]
}

// isPEMBase64Line reports whether s consists solely of PEM base64 characters.
func isPEMBase64Line(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '+' || c == '/' || c == '=') {
			return false
		}
	}
	return len(s) > 0
}
