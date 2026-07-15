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
	return "/" + strings.Join(segments, "/") + "/", nil
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
