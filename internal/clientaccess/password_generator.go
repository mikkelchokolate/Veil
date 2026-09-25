package clientaccess

import (
	"crypto/rand"
	"encoding/base64"
	"io"
)

type PasswordGenerator func() (string, error)

type InboundPasswordGenerator = PasswordGenerator

type ManagementPasswordGenerator struct {
	random io.Reader
}

func NewManagementPasswordGenerator(random io.Reader) ManagementPasswordGenerator {
	if random == nil {
		random = rand.Reader
	}
	return ManagementPasswordGenerator{random: random}
}

// Generate mints a random password. A crypto/rand failure fails closed: the
// historical "change-me" fallback installed a known credential (issue #1022).
func (g ManagementPasswordGenerator) Generate() (string, error) {
	buf := make([]byte, 9)
	if _, err := io.ReadFull(g.random, buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func generateInboundPassword() (string, error) {
	return NewManagementPasswordGenerator(rand.Reader).Generate()
}
