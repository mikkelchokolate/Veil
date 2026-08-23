package backup

import (
	"encoding/hex"
	"fmt"
	"os"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

const MinPassphraseLength = 16

// WriteNewPassphraseFile stores a new random backup passphrase at path with
// mode 0600. Callers must only use this when the file is missing; it replaces
// an existing file if one is already present.
func WriteNewPassphraseFile(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("backup passphrase path is required")
	}
	secret, err := randomPassphrase()
	if err != nil {
		return "", err
	}
	if err := atomicfile.Write(path, []byte(secret+"\n"), 0o600, 0o700); err != nil {
		return "", err
	}
	return secret, nil
}

// EnsurePassphraseFile writes a backup passphrase only when the path does not
// already exist, so install/repair never rotate an operator-configured secret.
func EnsurePassphraseFile(path string) error {
	if path == "" {
		return fmt.Errorf("backup passphrase path is required")
	}
	_, err := os.Lstat(path)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	_, err = WriteNewPassphraseFile(path)
	return err
}

func randomPassphrase() (string, error) {
	body := make([]byte, 32)
	if _, err := backupRandRead(body); err != nil {
		return "", err
	}
	return hex.EncodeToString(body), nil
}
