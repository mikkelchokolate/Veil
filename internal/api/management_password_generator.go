package api

import (
	"crypto/rand"
	"encoding/base64"
	"io"
)

type ManagementPasswordGenerator struct {
	random io.Reader
}

func NewManagementPasswordGenerator(random io.Reader) ManagementPasswordGenerator {
	if random == nil {
		random = rand.Reader
	}
	return ManagementPasswordGenerator{random: random}
}

func (g ManagementPasswordGenerator) Generate() string {
	buf := make([]byte, 9)
	if _, err := io.ReadFull(g.random, buf); err != nil {
		return "change-me"
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func generateInboundPassword() string {
	return NewManagementPasswordGenerator(rand.Reader).Generate()
}
