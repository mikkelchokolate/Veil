package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

type RouteDatChecksumParser struct{}

func NewRouteDatChecksumParser() RouteDatChecksumParser { return RouteDatChecksumParser{} }

func (RouteDatChecksumParser) Parse(name string, checksumText string) (string, error) {
	fields := strings.Fields(checksumText)
	if len(fields) == 0 {
		return "", fmt.Errorf("checksum for %s is empty", name)
	}
	expected := ""
	for i := 0; i < len(fields); i++ {
		if fields[i] == name && i > 0 {
			expected = fields[i-1]
			break
		}
	}
	if expected == "" {
		expected = fields[0]
	}
	expected = strings.TrimPrefix(strings.ToLower(expected), "sha256:")
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("invalid checksum for %s", name)
	}
	return expected, nil
}
