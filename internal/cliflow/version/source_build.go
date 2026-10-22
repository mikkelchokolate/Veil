package version

import "strings"

// SourceBuildCommit returns the full commit stamped by install-main, never an
// ambiguous short hash or an arbitrary dev build identity.
func SourceBuildCommit(v string) string {
	v = strings.TrimSpace(v)
	stamp, ok := strings.CutPrefix(v, "main-")
	if !ok {
		return ""
	}
	short, display, hasDisplay := strings.Cut(stamp, " (")
	if !hasDisplay {
		if len(stamp) == 40 && isHexCommit(stamp) {
			return strings.ToLower(stamp)
		}
		return ""
	}
	if !strings.HasSuffix(display, ")") {
		return ""
	}
	full := strings.TrimSuffix(display, ")")
	if len(full) != 40 || !isHexCommit(full) || !isHexCommit(short) ||
		!strings.HasPrefix(strings.ToLower(full), strings.ToLower(short)) {
		return ""
	}
	return strings.ToLower(full)
}
