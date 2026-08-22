package generatedconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

type RouteDatChecksumParser struct{}

func NewRouteDatChecksumParser() RouteDatChecksumParser { return RouteDatChecksumParser{} }

func (RouteDatChecksumParser) Parse(name string, checksumText string) (string, error) {
	trimmed := strings.TrimSpace(checksumText)
	if trimmed == "" {
		return "", fmt.Errorf("checksum for %s is empty", name)
	}
	lines := strings.Split(trimmed, "\n")
	expected := ""
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 1 && len(lines) == 1 {
			expected = fields[0]
			continue
		}
		if len(fields) != 2 {
			return "", fmt.Errorf("checksum for %s has an invalid record shape", name)
		}
		if strings.TrimPrefix(fields[1], "*") == name {
			if expected != "" {
				return "", fmt.Errorf("checksum manifest has duplicate records for %s", name)
			}
			expected = fields[0]
		}
	}
	if expected == "" {
		return "", fmt.Errorf("checksum record does not name %s", name)
	}
	expected = strings.TrimPrefix(strings.ToLower(expected), "sha256:")
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("invalid checksum for %s", name)
	}
	return expected, nil
}
