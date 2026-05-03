// Package secrets provides AES-256-GCM encryption for Veil's at-rest secrets.
// It implements field-level encryption with a version-prefixed format "ve1:<base64url>".
// Plaintext values pass through unchanged for backward compatibility.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

const (
	// Prefix is the version prefix for encrypted values.
	Prefix = "ve1:"

	// KeySize is the required length of AES-256 keys.
	KeySize = 32

	// NonceSize is the standard GCM nonce size.
	NonceSize = 12

	// TagSize is the GCM authentication tag size.
	TagSize = 16

	// MinCiphertextLen is the minimum raw ciphertext after decoding
	// (nonce + tag; GCM can encrypt empty plaintext).
	MinCiphertextLen = NonceSize + TagSize
)

// Cipher holds an AES-256-GCM AEAD instance for encrypting and decrypting
// individual secret fields.
type Cipher struct {
	aead cipher.AEAD
	key  [KeySize]byte
}

// NewCipher creates a new Cipher from a 32-byte AES-256 key.
func NewCipher(key [KeySize]byte) (*Cipher, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("secrets: aes.NewCipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: cipher.NewGCM: %w", err)
	}
	return &Cipher{aead: aead, key: key}, nil
}

// KeyBytes returns a copy of the cipher's key bytes.
func (c *Cipher) KeyBytes() []byte {
	out := make([]byte, KeySize)
	copy(out, c.key[:])
	return out
}

// Encrypt encrypts a plaintext string and returns a "ve1:<base64url>" string.
// Each call produces a unique ciphertext due to random nonce generation.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if c == nil {
		return "", errors.New("secrets: cipher is nil")
	}
	plainBytes := []byte(plaintext)
	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secrets: rand.Read nonce: %w", err)
	}
	// Seal appends encrypted data to nonce: output = nonce || ciphertext || tag
	ciphertext := c.aead.Seal(nonce, nonce, plainBytes, nil)
	encoded := base64.RawURLEncoding.EncodeToString(ciphertext)
	return Prefix + encoded, nil
}

// Decrypt decrypts a value. If the value does not start with "ve1:", it is
// returned as-is for backward compatibility with plaintext secrets.
func (c *Cipher) Decrypt(value string) (string, error) {
	if c == nil {
		return "", errors.New("secrets: cipher is nil")
	}
	if value == "" {
		return "", nil
	}
	if !IsEncrypted(value) {
		return value, nil
	}
	encoded := value[len(Prefix):]
	ciphertext, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("secrets: base64 decode: %w", err)
	}
	if len(ciphertext) < MinCiphertextLen {
		return "", fmt.Errorf("secrets: ciphertext too short: %d bytes (need at least %d)", len(ciphertext), MinCiphertextLen)
	}
	nonce := ciphertext[:NonceSize]
	payload := ciphertext[NonceSize:]
	plainBytes, err := c.aead.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", fmt.Errorf("secrets: GCM decrypt: %w", err)
	}
	return string(plainBytes), nil
}

// IsEncrypted reports whether a value has the encrypted prefix "ve1:".
func IsEncrypted(value string) bool {
	return len(value) > len(Prefix) && value[:len(Prefix)] == Prefix
}

// LoadOrCreateKey reads a 32-byte key from path. If the file does not exist,
// a new random key is generated and written with mode 0600. If the file exists
// but has wrong permissions, they are fixed to 0600. If the file exists but
// is not exactly 32 bytes, an error is returned.
func LoadOrCreateKey(path string) (*[KeySize]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("secrets: read key file %s: %w", path, err)
		}
		// File does not exist: generate new key
		return createKeyFile(path)
	}

	// Fix permissions if needed
	if info, err := os.Stat(path); err == nil {
		if info.Mode().Perm() != 0o600 {
			if err := os.Chmod(path, 0o600); err != nil {
				return nil, fmt.Errorf("secrets: chmod key file %s: %w", path, err)
			}
		}
	}

	if len(data) != KeySize {
		return nil, fmt.Errorf("secrets: key file %s has wrong length: %d bytes (expected %d)", path, len(data), KeySize)
	}

	var key [KeySize]byte
	copy(key[:], data)
	return &key, nil
}

func createKeyFile(path string) (*[KeySize]byte, error) {
	var key [KeySize]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, fmt.Errorf("secrets: generate key: %w", err)
	}
	if err := os.WriteFile(path, key[:], 0o600); err != nil {
		return nil, fmt.Errorf("secrets: write key file %s: %w", path, err)
	}
	return &key, nil
}
