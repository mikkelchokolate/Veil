package installer

import (
	"crypto/rand"
	"encoding/base64"
	"io"
)

type WebBasePathPolicy struct {
	random io.Reader
}

func NewWebBasePathPolicy(random io.Reader) WebBasePathPolicy {
	if random == nil {
		random = rand.Reader
	}
	return WebBasePathPolicy{random: random}
}

// Generate creates a random 12-character base64url string formatted as a Web base path.
func (p WebBasePathPolicy) Generate() string {
	buf := make([]byte, 9)
	if _, err := io.ReadFull(p.random, buf); err != nil {
		return "/veil-panel/"
	}
	return "/" + base64.RawURLEncoding.EncodeToString(buf) + "/"
}

func generateWebBasePath() string {
	return NewWebBasePathPolicy(rand.Reader).Generate()
}
