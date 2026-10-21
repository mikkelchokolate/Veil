package hostenv

import (
	"strconv"
	"strings"
)

// EnvBool parses a security-relevant boolean knob (VEIL_ACME_INSECURE,
// VEIL_AUTO_TLS, ...) with strconv.ParseBool semantics. Only an explicit true
// value ("1", "t", "true", ...) enables it; empty, false ("0", "false") and
// malformed values all report false. A bare non-empty check made
// VEIL_ACME_INSECURE=0 *disable* ACME TLS verification and persist "=1" into
// veil.env (issue #1263) — a typo or an explicit "off" must never fail open.
//
// Malformed-value policy, in one place: knobs whose true value WEAKENS
// security (VEIL_ACME_INSECURE) use EnvBool and treat malformed input as a
// silent false (fail closed). Knobs whose false value could silently drop a
// feature the operator asked for (VEIL_AUTO_TLS in `veil serve`) parse with
// strconv.ParseBool and reject malformed input at startup (fail loud), so
// spellings like "yes"/"on" are errors rather than guesses.
func EnvBool(value string) bool {
	enabled, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && enabled
}

// EnvBoolOrFile resolves a boolean knob the way EnvOrFile resolves strings:
// a non-empty process-environment value wins (so VEIL_ACME_INSECURE=0 in the
// operator's shell overrides a persisted =1), otherwise the persisted file
// value is used. The winning value is parsed with EnvBool.
func EnvBoolOrFile(fileValues map[string]string, key string) bool {
	return EnvBool(EnvOrFile(fileValues, key))
}
