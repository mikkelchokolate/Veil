package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/pbkdf2"
)

// Magic header for encrypted backups
var magicHeader = []byte("VEILBACK")

// encryptedArchiveHeaderBytes is the fixed encrypted-archive header: magic +
// version byte + 16-byte KDF salt + 12-byte nonce.
var encryptedArchiveHeaderBytes = len(magicHeader) + 1 + 16 + 12

// maxEncryptedArchivePrefixBytes bounds the accepted probe window; a probe
// is a small leading slice, so an oversized buffer is never a valid proof.
const maxEncryptedArchivePrefixBytes = 4096

// IsEncryptedArchivePrefix reports whether prefix contains a complete,
// well-formed Veil encrypted-archive header for a SUPPORTED encryption
// format version: magic, a version byte in {1,2,3}, the full salt+nonce
// header, and — for the chunked v3 stream — the first frame length must be
// present and within the 1 MiB frame bound. Truncated headers, unknown
// versions, oversized inputs, plaintext gzip and malformed prefixes are all
// rejected (#1223). The SFTP remote-destination boundary uses it to prove an
// archive really is encrypted before publishing it off-host — a .enc suffix
// is a naming convention, not a guarantee (#1188).
func IsEncryptedArchivePrefix(prefix []byte) bool {
	if len(prefix) < encryptedArchiveHeaderBytes || len(prefix) > maxEncryptedArchivePrefixBytes {
		return false
	}
	if !bytes.Equal(prefix[:len(magicHeader)], magicHeader) {
		return false
	}
	switch prefix[len(magicHeader)] {
	case 1, 2:
		// Blob format: header followed by GCM ciphertext — require at least
		// one byte of ciphertext (the tag alone is 16 bytes, so a header-only
		// prefix is incomplete).
		return len(prefix) > encryptedArchiveHeaderBytes
	case chunkedEncryptionVersion:
		// Chunked stream: the first frame's 4-byte big-endian length must be
		// present and within the frame bound.
		if len(prefix) < encryptedArchiveHeaderBytes+4 {
			return false
		}
		return binary.BigEndian.Uint32(prefix[encryptedArchiveHeaderBytes:]) <= backupChunkBytes
	default:
		return false
	}
}

// backupRandRead is overridable in tests to inject failures during encryption.
var backupRandRead = rand.Read

type DeriveKeyFunc func(passphrase string, salt []byte, version byte) []byte

type CryptoOptions struct {
	DeriveKey DeriveKeyFunc
}

var (
	createTarballStat        = os.Stat
	createTarballFileHeader  = tar.FileInfoHeader
	createTarballWriteHeader = (*tar.Writer).WriteHeader
	createTarballWrite       = (*tar.Writer).Write
	createTarballClose       = (*tar.Writer).Close
	createTarballGzipClose   = (*gzip.Writer).Close

	encryptAESNewCipher = aes.NewCipher
	encryptNewGCM       = cipher.NewGCM
)

func deriveKey(passphrase string, salt []byte, version byte) []byte {
	return deriveKeyWithOptions(passphrase, salt, version, CryptoOptions{})
}

func deriveKeyWithOptions(passphrase string, salt []byte, version byte, options CryptoOptions) []byte {
	if options.DeriveKey != nil {
		return options.DeriveKey(passphrase, salt, version)
	}
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
		info, err := createTarballStat(path)
		if err != nil {
			return err
		}
		hdr, err := createTarballFileHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = name
		hdr.Size = int64(len(data))
		if err := createTarballWriteHeader(tw, hdr); err != nil {
			return err
		}
		if _, err := createTarballWrite(tw, data); err != nil {
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

	if err := createTarballClose(tw); err != nil {
		return nil, err
	}
	if err := createTarballGzipClose(gw); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// CreateBackup creates the current archive format, inferring veil.db next to
// state.json. Restore remains backward compatible with legacy two-file v1
// archives.
func CreateBackup(statePath, keyPath, passphrase string) ([]byte, error) {
	return CreateBackupWithOptions(statePath, keyPath, passphrase, ArchiveOptions{
		DatabasePath: filepath.Join(filepath.Dir(statePath), "veil.db"),
	})
}

func encryptBackupTarball(tarball []byte, passphrase string) ([]byte, error) {
	return encryptBackupTarballWithOptions(tarball, passphrase, CryptoOptions{})
}

func encryptBackupTarballWithOptions(tarball []byte, passphrase string, options CryptoOptions) ([]byte, error) {
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

	key := deriveKeyWithOptions(passphrase, salt, 2, options)
	block, err := encryptAESNewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := encryptNewGCM(block)
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
