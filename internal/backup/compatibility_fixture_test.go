package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const compatibilityFixturePassphrase = "veil-compatibility-fixture"

var (
	compatibilityFixtureState = []byte(`{"schemaVersion":1,"settings":{"panelListen":"127.0.0.1:2096","panelAccess":"local","mode":"server"}}`)
	compatibilityFixtureKey   = bytes.Repeat([]byte{0x77}, 32)
)

func TestCommittedBackupCompatibilityFixtures(t *testing.T) {
	fixtures := []struct {
		name              string
		encryptionVersion int
	}{
		{name: "legacy-v1.enc", encryptionVersion: 1},
		{name: "v0.5.0-v2.enc", encryptionVersion: 2},
	}

	if os.Getenv("VEIL_UPDATE_BACKUP_FIXTURES") == "1" {
		writeCompatibilityFixtures(t, fixtures)
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", fixture.name))
			if err != nil {
				t.Fatal(err)
			}
			report, err := VerifyBackup(data, compatibilityFixturePassphrase)
			if err != nil {
				t.Fatal(err)
			}
			if !report.Encrypted || !report.Legacy ||
				report.EncryptionVersion != fixture.encryptionVersion ||
				report.StateSchemaVersion != 1 {
				t.Fatalf("verification report = %+v", report)
			}

			targetDir := t.TempDir()
			statePath := filepath.Join(targetDir, "state.json")
			keyPath := filepath.Join(targetDir, "state.key")
			if _, err := RestoreBackupWithOptions(
				data,
				statePath,
				keyPath,
				compatibilityFixturePassphrase,
				RestoreOptions{},
			); err != nil {
				t.Fatal(err)
			}
			state, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			key, err := os.ReadFile(keyPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(state, compatibilityFixtureState) ||
				!bytes.Equal(key, compatibilityFixtureKey) {
				t.Fatal("restored compatibility fixture does not match expected state and key")
			}
		})
	}
}

func writeCompatibilityFixtures(t *testing.T, fixtures []struct {
	name              string
	encryptionVersion int
}) {
	t.Helper()
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	tarball := compatibilityFixtureTarball(t)
	for _, fixture := range fixtures {
		data := encryptCompatibilityFixture(t, tarball, byte(fixture.encryptionVersion))
		if err := os.WriteFile(filepath.Join("testdata", fixture.name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func compatibilityFixtureTarball(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	gzipWriter.Header.ModTime = time.Unix(0, 0).UTC()
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range []struct {
		name string
		body []byte
	}{
		{name: "state.json", body: compatibilityFixtureState},
		{name: "state.key", body: compatibilityFixtureKey},
	} {
		header := &tar.Header{
			Name:     file.name,
			Mode:     0o600,
			Size:     int64(len(file.body)),
			Typeflag: tar.TypeReg,
			ModTime:  time.Unix(0, 0).UTC(),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(file.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func encryptCompatibilityFixture(t *testing.T, tarball []byte, version byte) []byte {
	t.Helper()
	salt := bytes.Repeat([]byte{0x12 + version}, 16)
	nonce := bytes.Repeat([]byte{0x34 + version}, 12)
	key := deriveKey(compatibilityFixturePassphrase, salt, version)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	var header bytes.Buffer
	header.Write(magicHeader)
	header.WriteByte(version)
	header.Write(salt)
	header.Write(nonce)
	var aad []byte
	if version >= 2 {
		aad = header.Bytes()
	}
	header.Write(aead.Seal(nil, nonce, tarball, aad))
	return header.Bytes()
}
