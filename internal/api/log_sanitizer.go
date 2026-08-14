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
	logSecretPattern        = regexp.MustCompile(`(?i)(\b(?:password|passwd|token|secret|private[_-]?key|license[_-]?key|authorization|key|auth_pass|basic_auth)\b["']?[ \t]*(?::|=|[ \t])[ \t]*["']?)[^ \t,\"'};]+`)
	logUserInfoPattern      = regexp.MustCompile(`(://[^:/\s]+:)[^@/\s]+(@)`)
	logUserInfoOnlyPattern  = regexp.MustCompile(`(://)[^@/:\s]+(@)`)
	logFragmentKeyPattern   = regexp.MustCompile(`(#)[0-9a-fA-F]{32,64}(\$)`)
	logPEMPrivateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	// Caddyfile basic_auth <user> <hash>: redact the credential hash.
	logBasicAuthPattern = regexp.MustCompile(`(?i)(\bbasic_auth\s+[A-Za-z0-9_.-]+\s+)[^ 	\n{]+`)
	// Multi-line YAML secret values: redact only the value on the key's line,
	// never the rest of the document (audit #179/#186: a greedy \s* pattern
	// crossing newlines ate "type: password" and left the secret in place).
	logYAMLSecretPattern = regexp.MustCompile(`(?m)^(\s*(?:password|passwd|token|secret|key|auth_pass|auth_credentials)\s*:\s*)([^#\n][^\n]*)$`)
	// hysteria2 userpass map: "alice: SECRET" under an arbitrary username.
	// A 4-space indent (nested under "userpass:" inside "auth:") and a
	// letter-initial username exclude timestamps and structural YAML keys
	// (type:, cipher:, ...) from being treated as user:secret pairs.
	logUserPassMapPattern = regexp.MustCompile(`(?m)^( {4,}[A-Za-z][A-Za-z0-9_.-]*\s*:\s*)([^#\n][^\n]*)$`)
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
	output = logUserPassMapPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret)
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
