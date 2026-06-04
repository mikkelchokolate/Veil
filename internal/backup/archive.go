package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/pbkdf2"
)

// Magic header for encrypted backups
var magicHeader = []byte("VEILBACK")

func deriveKey(passphrase string, salt []byte) []byte {
	return pbkdf2.Key([]byte(passphrase), salt, 10000, 32, sha256.New)
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
	tarball, err := createTarball(statePath, keyPath)
	if err != nil {
		return nil, err
	}

	if passphrase == "" {
		return tarball, nil
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	key := deriveKey(passphrase, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	ciphertext := aead.Seal(nil, nonce, tarball, nil)

	var out bytes.Buffer
	out.Write(magicHeader)
	out.WriteByte(1) // version
	out.Write(salt)
	out.Write(nonce)
	out.Write(ciphertext)

	return out.Bytes(), nil
}

// RestoreBackup restores state.json and state.key from backup data.
// It decrypts the data first if the backup is encrypted.
func RestoreBackup(data []byte, statePath, keyPath, passphrase string) error {
	var tarball []byte

	if len(data) >= len(magicHeader) && bytes.Equal(data[:len(magicHeader)], magicHeader) {
		if passphrase == "" {
			return errors.New("passphrase is required to decrypt this backup")
		}
		headerLen := len(magicHeader) + 1 + 16 + 12
		if len(data) < headerLen {
			return errors.New("invalid or corrupted encrypted backup file (too short)")
		}
		version := data[len(magicHeader)]
		if version != 1 {
			return fmt.Errorf("unsupported backup format version: %d", version)
		}
		salt := data[len(magicHeader)+1 : len(magicHeader)+1+16]
		nonce := data[len(magicHeader)+1+16 : headerLen]
		ciphertext := data[headerLen:]

		key := deriveKey(passphrase, salt)
		block, err := aes.NewCipher(key)
		if err != nil {
			return err
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return err
		}

		decrypted, err := aead.Open(nil, nonce, ciphertext, nil)
		if err != nil {
			return errors.New("failed to decrypt backup: incorrect passphrase or corrupted data")
		}
		tarball = decrypted
	} else {
		if passphrase != "" {
			return errors.New("passphrase provided but backup is not encrypted")
		}
		tarball = data
	}

	// Unpack gzip
	gzr, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		return fmt.Errorf("initialize gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var stateData, keyData []byte
	var stateMode, keyMode os.FileMode
	stateMode = 0o600
	keyMode = 0o600

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar archive: %w", err)
		}

		var content bytes.Buffer
		if _, err := io.Copy(&content, tr); err != nil {
			return fmt.Errorf("extract file %s: %w", hdr.Name, err)
		}

		switch hdr.Name {
		case "state.json":
			stateData = content.Bytes()
			if hdr.FileInfo().Mode() != 0 {
				stateMode = hdr.FileInfo().Mode().Perm()
			}
		case "state.key":
			keyData = content.Bytes()
			if hdr.FileInfo().Mode() != 0 {
				keyMode = hdr.FileInfo().Mode().Perm()
			}
		}
	}

	if len(stateData) == 0 {
		return errors.New("invalid backup: missing state.json")
	}
	if len(keyData) == 0 {
		return errors.New("invalid backup: missing state.key")
	}

	writeAtomic := func(dest string, payload []byte, mode os.FileMode) error {
		dir := filepath.Dir(dest)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
		tmpFile := dest + ".tmp"
		if err := os.WriteFile(tmpFile, payload, mode); err != nil {
			return fmt.Errorf("write temp file %s: %w", tmpFile, err)
		}
		if err := os.Rename(tmpFile, dest); err != nil {
			os.Remove(tmpFile)
			return fmt.Errorf("rename to %s: %w", dest, err)
		}
		return nil
	}

	if err := writeAtomic(statePath, stateData, stateMode); err != nil {
		return err
	}
	if err := writeAtomic(keyPath, keyData, keyMode); err != nil {
		return err
	}

	return nil
}
