package generatedconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

type RouteDatChecksumVerifier struct{}

func NewRouteDatChecksumVerifier() RouteDatChecksumVerifier { return RouteDatChecksumVerifier{} }

func (RouteDatChecksumVerifier) Verify(name string, body []byte, checksumText string) error {
	expected, err := NewRouteDatChecksumParser().Parse(name, checksumText)
	if err != nil {
		return err
	}
	actual := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(actual[:]), expected) {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}

func VerifyRouteDatChecksum(name string, body []byte, checksumText string) error {
	return NewRouteDatChecksumVerifier().Verify(name, body, checksumText)
}

func verifyRouteDatChecksum(name string, body []byte, checksumText string) error {
	return VerifyRouteDatChecksum(name, body, checksumText)
}
