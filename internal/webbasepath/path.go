package webbasepath

import (
	"fmt"
	"strings"
)

// Normalize returns a canonical absolute mount path with a trailing slash.
// Root is represented as "/". Restricting segments to URL-safe ASCII keeps the
// same value safe in HTTP routing, proxy configuration, HTML attributes, and
// quoted JavaScript request paths.
func Normalize(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return "/", nil
	}

	trimmed := strings.Trim(value, "/")
	if trimmed == "" {
		return "/", nil
	}
	segments := strings.Split(trimmed, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("must not contain empty, '.' or '..' segments")
		}
		for _, r := range segment {
			if !isSafeSegmentRune(r) {
				return "", fmt.Errorf("segment %q contains unsupported character %q", segment, r)
			}
		}
	}
	// A reserved FIRST segment collides with a route registered on the root
	// mux before stripBasePathMiddleware runs (issues #662, #765):
	//   - "s" is the public /s/{token} subscription bypass: panel URLs under
	//     an /s/… mount would be misclassified as feeds and never stripped,
	//     and the session cookie scoped to /s/… would leak onto the public
	//     endpoint.
	//   - "api", "metrics", "healthz", "livez", "readyz", "assets", and the
	//     panel file names are fixed root mounts: mounting at /api/ serves
	//     the SPA at /api and moves the real API to /api/api/…, while a
	//     /metrics/ mount answers scrapes with panel HTML instead of
	//     Prometheus output.
	if reason, reserved := reservedFirstSegments[segments[0]]; reserved {
		return "", fmt.Errorf("first segment %q is reserved for %s", segments[0], reason)
	}
	return "/" + strings.Join(segments, "/") + "/", nil
}

// reservedFirstSegments maps each root-mux first segment to a short reason
// used in validation errors. Only the first segment is checked: mounts like
// /panel/api/… cannot collide because requests outside the mount never reach
// the router at all.
var reservedFirstSegments = map[string]string{
	"s":           "public subscription links",
	"api":         "the management API",
	"metrics":     "the Prometheus metrics endpoint",
	"healthz":     "the health check endpoint",
	"livez":       "the liveness endpoint",
	"readyz":      "the readiness endpoint",
	"assets":      "the static asset mount",
	"favicon.ico": "the favicon endpoint",
	"favicon.svg": "the favicon endpoint",
	"robots.txt":  "the robots.txt endpoint",
}

// NormalizeOptional uses the settings representation where root is stored as
// an empty string.
func NormalizeOptional(value string) (string, error) {
	normalized, err := Normalize(value)
	if err != nil {
		return "", err
	}
	if normalized == "/" {
		return "", nil
	}
	return normalized, nil
}

func isSafeSegmentRune(r rune) bool {
	return r >= 'a' && r <= 'z' ||
		r >= 'A' && r <= 'Z' ||
		r >= '0' && r <= '9' ||
		r == '-' || r == '_' || r == '.' || r == '~'
}
