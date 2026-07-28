package api

import (
	"regexp"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/settings"
)

const maxServiceLogResponseBytes = 256 * 1024

var (
	logBearerPattern        = regexp.MustCompile(`(?i)(\bbearer\s+)[A-Za-z0-9._~+/=-]+`)
	logJSONSecretPattern    = regexp.MustCompile(`(?i)("(?:password|passwd|token|secret|private[_-]?key|license[_-]?key|authorization)"\s*:\s*")[^"]*(")`)
	logSecretPattern        = regexp.MustCompile(`(?i)((?:password|passwd|token|secret|private[_-]?key|license[_-]?key|authorization)["']?\s*(?::|=|\s)\s*["']?)[^\s,"'};]+`)
	logUserInfoPattern      = regexp.MustCompile(`(://[^:/\s]+:)[^@/\s]+(@)`)
	logPEMPrivateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
)

func sanitizeServiceLogOutput(output string) string {
	output = logPEMPrivateKeyPattern.ReplaceAllString(output, settings.RedactedSecret)
	output = logBearerPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret)
	output = logJSONSecretPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret+`${2}`)
	output = logSecretPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret)
	output = logUserInfoPattern.ReplaceAllString(output, `${1}`+settings.RedactedSecret+`${2}`)
	if len(output) <= maxServiceLogResponseBytes {
		return output
	}
	const marker = "\n...[TRUNCATED]"
	prefix := strings.ToValidUTF8(output[:maxServiceLogResponseBytes-len(marker)], "")
	return strings.TrimRight(prefix, "\x00") + marker
}
