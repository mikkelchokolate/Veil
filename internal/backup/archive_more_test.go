package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateTarballErrors(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")

	// Missing state file.
	if _, err := createTarball(statePath, keyPath); err == nil {
		t.Fatal("expected error for missing state file")
	}

	if err := os.WriteFile(statePath, []byte(`{"schemaVersion":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Missing key file.
	if _, err := createTarball(statePath, keyPath); err == nil {
		t.Fatal("expected error for missing key file")
	}
}

func TestEncryptBackupTarballRandFailure(t *testing.T) {
	tests := []struct {
		name      string
		failAfter int
	}{
		{"salt fails", 0},
		{"nonce fails", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := backupRandRead
			defer func() { backupRandRead = original }()

			calls := 0
			backupRandRead = func(b []byte) (int, error) {
				if calls == tt.failAfter {
					return 0, errors.New("injected rand failure")
				}
				calls++
				return rand.Read(b)
			}

			if _, err := encryptBackupTarball([]byte("tarball"), "passphrase"); err == nil {
				t.Fatal("expected error from rand failure")
			}
		})
	}
}

func TestDecryptBackupEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		passphrase string
		wantErr    string
	}{
		{
			name:       "too short for header",
			data:       append(bytes.Clone(magicHeader), byte(2)),
			passphrase: "pass",
			wantErr:    "too short",
		},
		{
			name:       "unsupported version",
			data:       append(bytes.Clone(magicHeader), append([]byte{byte(3)}, make([]byte, 28)...)...),
			passphrase: "pass",
			wantErr:    "unsupported backup format version",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := decryptBackup(tt.data, tt.passphrase)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestReadArchiveTarballValidation(t *testing.T) {
	state := []byte(`{"schemaVersion":1}`)
	key := bytes.Repeat([]byte{0x42}, 32)
	manifest := []byte(`{"formatVersion":1}`)

	tests := []struct {
		name    string
		entries []tarEntry
		wantErr string
	}{
		{
			name:    "unexpected entry",
			entries: []tarEntry{{name: "unexpected.txt", body: []byte("x")}},
			wantErr: "unexpected archive entry",
		},
		{
			name: "duplicate state.json",
			entries: []tarEntry{
				{name: "state.json", body: state},
				{name: "state.json", body: state},
			},
			wantErr: "duplicate archive entry",
		},
		{
			name:    "non-regular file",
			entries: []tarEntry{{name: "state.json", typeflag: tar.TypeDir}},
			wantErr: "not a regular file",
		},
		{
			name:    "size limit exceeded",
			entries: []tarEntry{{name: "state.json", body: bytes.Repeat([]byte{0x42}, maxBackupArchiveFileBytes+1)}},
			wantErr: "exceeds size limit",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildTarball(t, tt.entries)
			_, err := readArchiveTarball(data)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}

	// Valid tarball with all expected files should succeed.
	t.Run("valid", func(t *testing.T) {
		data := buildTarball(t, []tarEntry{
			{name: "state.json", body: state},
			{name: "state.key", body: key},
			{name: "manifest.json", body: manifest},
		})
		contents, err := readArchiveTarball(data)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(contents.state, state) || !bytes.Equal(contents.key, key) || !bytes.Equal(contents.manifest, manifest) {
			t.Fatalf("unexpected contents: %+v", contents)
		}
	})
}

type tarEntry struct {
	name     string
	body     []byte
	typeflag byte
	size     int64
}

func buildTarball(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, e := range entries {
		size := e.size
		if size == 0 {
			size = int64(len(e.body))
		}
		tf := e.typeflag
		if tf == 0 {
			tf = tar.TypeReg
		}
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     0o600,
			Size:     size,
			Typeflag: tf,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// buildRawTarball creates a tar.gz where the header for the single entry claims
// the supplied size/typeflag but only the provided body bytes are written.
// This is useful for testing reader validation without tar.Writer enforcing
// header/body consistency.
func buildRawTarball(t *testing.T, name string, size int64, typeflag byte, body []byte) []byte {
	t.Helper()
	var header [512]byte
	copy(header[0:100], name)
	copy(header[100:108], fmt.Sprintf("%07o", 0o600))
	copy(header[108:116], fmt.Sprintf("%07o", 0))
	copy(header[116:124], fmt.Sprintf("%07o", 0))
	copy(header[124:136], fmt.Sprintf("%011o", size))
	copy(header[136:148], fmt.Sprintf("%011o", 0))
	// checksum field is initially spaces for calculation.
	for i := 148; i < 156; i++ {
		header[i] = ' '
	}
	header[156] = typeflag
	copy(header[257:263], "ustar")
	copy(header[263:265], "00")

	var sum int
	for _, b := range header {
		sum += int(b)
	}
	copy(header[148:156], fmt.Sprintf("%06o", sum))
	header[155] = 0

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(header[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadArchiveTarballTruncatedEntry(t *testing.T) {
	state := []byte(`{"schemaVersion":1}`)
	data := buildRawTarball(t, "state.json", int64(len(state))+5, tar.TypeReg, state)
	_, err := readArchiveTarball(data)
	if err == nil || !strings.Contains(err.Error(), "read archive entry \"state.json\"") {
		t.Fatalf("expected read archive entry error, got %v", err)
	}
}

func TestVerifyManifestFilesMismatches(t *testing.T) {
	files := []ArchiveFile{
		{Name: "state.json", Size: 10, SHA256: strings.Repeat("0", 64)},
		{Name: "state.key", Size: 32, SHA256: strings.Repeat("1", 64)},
	}

	tests := []struct {
		name    string
		mutate  func(*[]ArchiveFile)
		wantErr string
	}{
		{
			name: "name mismatch",
			mutate: func(f *[]ArchiveFile) {
				(*f)[0].Name = "other.json"
			},
			wantErr: "checksum mismatch",
		},
		{
			name: "size mismatch",
			mutate: func(f *[]ArchiveFile) {
				(*f)[0].Size = 99
			},
			wantErr: "checksum mismatch",
		},
		{
			name: "sha256 mismatch",
			mutate: func(f *[]ArchiveFile) {
				(*f)[0].SHA256 = strings.Repeat("f", 64)
			},
			wantErr: "checksum mismatch",
		},
		{
			name: "extra file",
			mutate: func(f *[]ArchiveFile) {
				*f = append(*f, ArchiveFile{Name: "extra", Size: 1, SHA256: strings.Repeat("2", 64)})
			},
			wantErr: "file count mismatch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expected := make([]ArchiveFile, len(files))
			copy(expected, files)
			actual := make([]ArchiveFile, len(files))
			copy(actual, files)
			tt.mutate(&expected)
			err := verifyManifestFiles(expected, actual)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestRawStateSchemaVersionInvalidJSON(t *testing.T) {
	if got := rawStateSchemaVersion([]byte("not json")); got != 1 {
		t.Fatalf("expected 1 for invalid JSON, got %d", got)
	}
	if got := rawStateSchemaVersion([]byte(`{"schemaVersion":-5}`)); got != 1 {
		t.Fatalf("expected 1 for non-positive schema, got %d", got)
	}
}

func TestCreateTarballWriterErrors(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")
	if err := os.WriteFile(statePath, []byte(`{"schemaVersion":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x42}, 32), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		inject  func() (cleanup func())
		wantErr string
	}{
		{
			name: "stat error",
			inject: func() (cleanup func()) {
				orig := createTarballStat
				createTarballStat = func(string) (os.FileInfo, error) {
					return nil, errors.New("injected stat error")
				}
				return func() { createTarballStat = orig }
			},
			wantErr: "injected stat error",
		},
		{
			name: "file header error",
			inject: func() (cleanup func()) {
				orig := createTarballFileHeader
				createTarballFileHeader = func(os.FileInfo, string) (*tar.Header, error) {
					return nil, errors.New("injected header error")
				}
				return func() { createTarballFileHeader = orig }
			},
			wantErr: "injected header error",
		},
		{
			name: "write header error",
			inject: func() (cleanup func()) {
				orig := createTarballWriteHeader
				createTarballWriteHeader = func(*tar.Writer, *tar.Header) error {
					return errors.New("injected write header error")
				}
				return func() { createTarballWriteHeader = orig }
			},
			wantErr: "injected write header error",
		},
		{
			name: "write error",
			inject: func() (cleanup func()) {
				orig := createTarballWrite
				createTarballWrite = func(*tar.Writer, []byte) (int, error) {
					return 0, errors.New("injected write error")
				}
				return func() { createTarballWrite = orig }
			},
			wantErr: "injected write error",
		},
		{
			name: "tar close error",
			inject: func() (cleanup func()) {
				orig := createTarballClose
				createTarballClose = func(*tar.Writer) error {
					return errors.New("injected tar close error")
				}
				return func() { createTarballClose = orig }
			},
			wantErr: "injected tar close error",
		},
		{
			name: "gzip close error",
			inject: func() (cleanup func()) {
				orig := createTarballGzipClose
				createTarballGzipClose = func(*gzip.Writer) error {
					return errors.New("injected gzip close error")
				}
				return func() { createTarballGzipClose = orig }
			},
			wantErr: "injected gzip close error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := tt.inject()
			defer cleanup()
			_, err := createTarball(statePath, keyPath)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestEncryptBackupTarballCipherErrors(t *testing.T) {
	fastKey := func(string, []byte, byte) []byte { return bytes.Repeat([]byte{0xab}, 32) }

	t.Run("aes new cipher error", func(t *testing.T) {
		origDerive := deriveKeyHook
		orig := encryptAESNewCipher
		defer func() {
			deriveKeyHook = origDerive
			encryptAESNewCipher = orig
		}()
		deriveKeyHook = fastKey
		encryptAESNewCipher = func([]byte) (cipher.Block, error) {
			return nil, errors.New("injected aes error")
		}
		if _, err := encryptBackupTarball([]byte("tarball"), "passphrase"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("gcm error", func(t *testing.T) {
		origDerive := deriveKeyHook
		orig := encryptNewGCM
		defer func() {
			deriveKeyHook = origDerive
			encryptNewGCM = orig
		}()
		deriveKeyHook = fastKey
		encryptNewGCM = func(cipher.Block) (cipher.AEAD, error) {
			return nil, errors.New("injected gcm error")
		}
		if _, err := encryptBackupTarball([]byte("tarball"), "passphrase"); err == nil {
			t.Fatal("expected error")
		}
	})
}
