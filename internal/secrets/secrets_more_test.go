package secrets

import (
	"crypto/cipher"
	"errors"
	"testing"
)

func TestNewCipherAESNewCipherError(t *testing.T) {
	old := aesNewCipher
	aesNewCipher = func(key []byte) (cipher.Block, error) { return nil, errors.New("injected aes.NewCipher error") }
	defer func() { aesNewCipher = old }()

	var key [KeySize]byte
	_, err := NewCipher(key)
	if err == nil {
		t.Fatal("expected error when aes.NewCipher fails")
	}
}

func TestNewCipherNewGCMError(t *testing.T) {
	old := newGCM
	newGCM = func(block cipher.Block) (cipher.AEAD, error) { return nil, errors.New("injected cipher.NewGCM error") }
	defer func() { newGCM = old }()

	var key [KeySize]byte
	_, err := NewCipher(key)
	if err == nil {
		t.Fatal("expected error when cipher.NewGCM fails")
	}
}

func TestEncryptRandReadError(t *testing.T) {
	old := randRead
	randRead = func(b []byte) (int, error) { return 0, errors.New("injected rand.Read error") }
	defer func() { randRead = old }()

	var key [KeySize]byte
	c, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	_, err = c.Encrypt("test")
	if err == nil {
		t.Fatal("expected error when nonce rand.Read fails")
	}
}
