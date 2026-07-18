package client

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/secrets"
)

func newTestCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	var key [secrets.KeySize]byte
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	return c
}
