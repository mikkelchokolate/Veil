package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"

	"golang.org/x/crypto/pbkdf2"
)

// Magic header for encrypted backups
var magicHeader = []byte("VEILBACK")

// backupRandRead is overridable in tests to inject failures during encryption.
var backupRandRead = rand.Read

func deriveKey(passphrase string, salt []byte, version byte) []byte {
	iterations := 600000 // OWASP recommendation for PBKDF2-HMAC-SHA256
	if version == 1 {
		iterations = 10000 // Legacy version
	}
	return pbkdf2.Key([]byte(passphrase), salt, iterations, 32, sha256.New)
}

func createTarball(statePath, keyPath string) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	addFile := func(path string, name string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = name
		hdr.Size = int64(len(data))
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
		return nil
	}

	if err := addFile(statePath, "state.json"); err != nil {
		return nil, fmt.Errorf("archive state: %w", err)
	}
	if err := addFile(keyPath, "state.key"); err != nil {
		return nil, fmt.Errorf("archive key: %w", err)
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// CreateBackup packages state.json and state.key into a tar.gz archive.
// If a non-empty passphrase is provided, the archive is encrypted using PBKDF2 + AES-GCM.
func CreateBackup(statePath, keyPath, passphrase string) ([]byte, error) {
	return CreateBackupWithOptions(statePath, keyPath, passphrase, ArchiveOptions{})
}

func encryptBackupTarball(tarball []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return tarball, nil
	}

	salt := make([]byte, 16)
	if _, err := backupRandRead(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	nonce := make([]byte, 12)
	if _, err := backupRandRead(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	key := deriveKey(passphrase, salt, 2)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	out.Write(magicHeader)
	out.WriteByte(2) // version 2 uses 600k iterations and authenticated header (AAD)
	out.Write(salt)
	out.Write(nonce)

	headerBytes := out.Bytes()
	ciphertext := aead.Seal(nil, nonce, tarball, headerBytes)
	out.Write(ciphertext)

	return out.Bytes(), nil
}

// RestoreBackup restores state.json and state.key from backup data.
// It decrypts the data first if the backup is encrypted.
func RestoreBackup(data []byte, statePath, keyPath, passphrase string) error {
	_, err := RestoreBackupWithOptions(data, statePath, keyPath, passphrase, RestoreOptions{})
	return err
}
