package backup

import "crypto/sha256"

// fastTestCryptoOptions is deliberately scoped to a single operation. It is
// not installed in production and does not alter archive version semantics.
func fastTestCryptoOptions() CryptoOptions {
	return CryptoOptions{DeriveKey: func(passphrase string, salt []byte, version byte) []byte {
		h := sha256.New()
		h.Write([]byte(passphrase))
		h.Write(salt)
		h.Write([]byte{version})
		return h.Sum(nil)
	}}
}
