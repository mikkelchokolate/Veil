package api

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/veil-panel/veil/internal/secrets"
)

var _managementTestDeps_state_secrets = []any{
	bytes.Buffer{}, rand.Reader, fmt.Sprintf, log.Printf, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{}, time.Second, secrets.IsEncrypted,
}

func TestManagementStateLoadReturnsErrorOnCorruptedState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "management-state.json")

	// Write corrupted (invalid JSON) state file
	if err := os.WriteFile(statePath, []byte("not valid json {{{"), 0o600); err != nil {
		t.Fatalf("write corrupted state: %v", err)
	}

	state := &managementState{statePath: statePath}
	err := state.load()
	if err == nil {
		t.Fatal("expected error for corrupted state file, got nil")
	}
	if !strings.Contains(err.Error(), "invalid character") && !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected error to mention invalid character or unmarshal, got: %v", err)
	}
}

func TestNewManagementStateLogsCorruptedStateError(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "management-state.json")

	// Write corrupted (invalid JSON) state file
	if err := os.WriteFile(statePath, []byte("not valid json {{{"), 0o600); err != nil {
		t.Fatalf("write corrupted state: %v", err)
	}

	// Capture log output
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	// Create management state via NewRouter (which calls newManagementState internally)
	_, _ = NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	output := buf.String()
	if output == "" {
		t.Fatal("expected log output when loading corrupted state file, but log was empty")
	}
	if !strings.Contains(output, "management-state.json") {
		t.Fatalf("expected log output to mention state file, got: %s", output)
	}
}

func TestEncryptSnapshotEncryptsAllPlaintextFields(t *testing.T) {
	cipher := newTestCipher(t)
	s := &managementState{cipher: cipher}
	snapshot := newTestSnapshot()

	s.encryptSnapshot(&snapshot)

	// All 4 fields should be encrypted (start with "ve1:")
	if !secrets.IsEncrypted(snapshot.Settings.NaivePassword) {
		t.Fatal("NaivePassword was not encrypted")
	}
	if !secrets.IsEncrypted(snapshot.Settings.Hysteria2Password) {
		t.Fatal("Hysteria2Password was not encrypted")
	}
	if !secrets.IsEncrypted(snapshot.Warp.LicenseKey) {
		t.Fatal("Warp.LicenseKey was not encrypted")
	}
	if !secrets.IsEncrypted(snapshot.Warp.PrivateKey) {
		t.Fatal("Warp.PrivateKey was not encrypted")
	}

	// Verify each field decrypts back to original plaintext
	if dec, err := cipher.Decrypt(snapshot.Settings.NaivePassword); err != nil {
		t.Fatalf("failed to decrypt NaivePassword: %v", err)
	} else if dec != "naive-password-plain" {
		t.Fatalf("decrypted NaivePassword = %q, want %q", dec, "naive-password-plain")
	}
	if dec, err := cipher.Decrypt(snapshot.Settings.Hysteria2Password); err != nil {
		t.Fatalf("failed to decrypt Hysteria2Password: %v", err)
	} else if dec != "hysteria2-password-plain" {
		t.Fatalf("decrypted Hysteria2Password = %q, want %q", dec, "hysteria2-password-plain")
	}
	if dec, err := cipher.Decrypt(snapshot.Warp.LicenseKey); err != nil {
		t.Fatalf("failed to decrypt Warp.LicenseKey: %v", err)
	} else if dec != "warp-license-plain" {
		t.Fatalf("decrypted Warp.LicenseKey = %q, want %q", dec, "warp-license-plain")
	}
	if dec, err := cipher.Decrypt(snapshot.Warp.PrivateKey); err != nil {
		t.Fatalf("failed to decrypt Warp.PrivateKey: %v", err)
	} else if dec != "warp-private-plain" {
		t.Fatalf("decrypted Warp.PrivateKey = %q, want %q", dec, "warp-private-plain")
	}
}

func TestEncryptSnapshotSkipsAlreadyEncryptedFields(t *testing.T) {
	cipher := newTestCipher(t)
	s := &managementState{cipher: cipher}

	alreadyEncrypted := "ve1:some-already-encrypted-value"
	snapshot := managementSnapshot{
		Settings: Settings{
			NaivePassword:     alreadyEncrypted,
			Hysteria2Password: "hysteria2-password-plain",
		},
		Warp: WarpConfig{
			LicenseKey: alreadyEncrypted,
			PrivateKey: "warp-private-plain",
		},
	}

	s.encryptSnapshot(&snapshot)

	// Already-encrypted fields must remain unchanged
	if snapshot.Settings.NaivePassword != alreadyEncrypted {
		t.Fatalf("already-encrypted NaivePassword was modified: %q", snapshot.Settings.NaivePassword)
	}
	if snapshot.Warp.LicenseKey != alreadyEncrypted {
		t.Fatalf("already-encrypted Warp.LicenseKey was modified: %q", snapshot.Warp.LicenseKey)
	}

	// Plaintext fields should be encrypted
	if !secrets.IsEncrypted(snapshot.Settings.Hysteria2Password) {
		t.Fatal("Hysteria2Password should have been encrypted")
	}
	if !secrets.IsEncrypted(snapshot.Warp.PrivateKey) {
		t.Fatal("Warp.PrivateKey should have been encrypted")
	}
}

func TestEncryptSnapshotSkipsEmptyFields(t *testing.T) {
	cipher := newTestCipher(t)
	s := &managementState{cipher: cipher}

	snapshot := managementSnapshot{
		Settings: Settings{
			NaivePassword:     "",
			Hysteria2Password: "",
		},
		Warp: WarpConfig{
			LicenseKey: "",
			PrivateKey: "",
		},
	}

	s.encryptSnapshot(&snapshot)

	// Empty fields must remain empty
	if snapshot.Settings.NaivePassword != "" {
		t.Fatalf("empty NaivePassword was modified: %q", snapshot.Settings.NaivePassword)
	}
	if snapshot.Settings.Hysteria2Password != "" {
		t.Fatalf("empty Hysteria2Password was modified: %q", snapshot.Settings.Hysteria2Password)
	}
	if snapshot.Warp.LicenseKey != "" {
		t.Fatalf("empty Warp.LicenseKey was modified: %q", snapshot.Warp.LicenseKey)
	}
	if snapshot.Warp.PrivateKey != "" {
		t.Fatalf("empty Warp.PrivateKey was modified: %q", snapshot.Warp.PrivateKey)
	}
}

func TestEncryptSnapshotNoopWhenCipherIsNil(t *testing.T) {
	s := &managementState{cipher: nil} // cipher is nil
	snapshot := newTestSnapshot()

	s.encryptSnapshot(&snapshot)

	// All fields should remain as plaintext (no-op)
	if snapshot.Settings.NaivePassword != "naive-password-plain" {
		t.Fatalf("NaivePassword was modified with nil cipher: %q", snapshot.Settings.NaivePassword)
	}
	if snapshot.Settings.Hysteria2Password != "hysteria2-password-plain" {
		t.Fatalf("Hysteria2Password was modified with nil cipher: %q", snapshot.Settings.Hysteria2Password)
	}
	if snapshot.Warp.LicenseKey != "warp-license-plain" {
		t.Fatalf("Warp.LicenseKey was modified with nil cipher: %q", snapshot.Warp.LicenseKey)
	}
	if snapshot.Warp.PrivateKey != "warp-private-plain" {
		t.Fatalf("Warp.PrivateKey was modified with nil cipher: %q", snapshot.Warp.PrivateKey)
	}
}

func TestDecryptSnapshotRestoresPlaintext(t *testing.T) {
	cipher := newTestCipher(t)
	s := &managementState{cipher: cipher}
	snapshot := newTestSnapshot()

	// First encrypt
	s.encryptSnapshot(&snapshot)

	// Verify they're encrypted
	if !secrets.IsEncrypted(snapshot.Settings.NaivePassword) {
		t.Fatal("expected NaivePassword to be encrypted before decrypt")
	}

	// Then decrypt
	s.decryptSnapshot(&snapshot)

	// All fields should be back to plaintext
	if snapshot.Settings.NaivePassword != "naive-password-plain" {
		t.Fatalf("decrypted NaivePassword = %q, want %q", snapshot.Settings.NaivePassword, "naive-password-plain")
	}
	if snapshot.Settings.Hysteria2Password != "hysteria2-password-plain" {
		t.Fatalf("decrypted Hysteria2Password = %q, want %q", snapshot.Settings.Hysteria2Password, "hysteria2-password-plain")
	}
	if snapshot.Warp.LicenseKey != "warp-license-plain" {
		t.Fatalf("decrypted Warp.LicenseKey = %q, want %q", snapshot.Warp.LicenseKey, "warp-license-plain")
	}
	if snapshot.Warp.PrivateKey != "warp-private-plain" {
		t.Fatalf("decrypted Warp.PrivateKey = %q, want %q", snapshot.Warp.PrivateKey, "warp-private-plain")
	}
}

func TestDecryptSnapshotNoopWhenCipherIsNil(t *testing.T) {
	s := &managementState{cipher: nil}
	snapshot := newTestSnapshot()

	s.decryptSnapshot(&snapshot)

	// All fields should remain unchanged
	if snapshot.Settings.NaivePassword != "naive-password-plain" {
		t.Fatalf("NaivePassword was modified with nil cipher: %q", snapshot.Settings.NaivePassword)
	}
	if snapshot.Settings.Hysteria2Password != "hysteria2-password-plain" {
		t.Fatalf("Hysteria2Password was modified with nil cipher: %q", snapshot.Settings.Hysteria2Password)
	}
	if snapshot.Warp.LicenseKey != "warp-license-plain" {
		t.Fatalf("Warp.LicenseKey was modified with nil cipher: %q", snapshot.Warp.LicenseKey)
	}
	if snapshot.Warp.PrivateKey != "warp-private-plain" {
		t.Fatalf("Warp.PrivateKey was modified with nil cipher: %q", snapshot.Warp.PrivateKey)
	}
}

func TestDecryptSnapshotPassesThroughPlaintext(t *testing.T) {
	cipher := newTestCipher(t)
	s := &managementState{cipher: cipher}
	snapshot := newTestSnapshot()

	// decryptSnapshot without encrypting first — plaintext should pass through unchanged
	s.decryptSnapshot(&snapshot)

	// All plaintext fields should remain unchanged (pass through)
	if snapshot.Settings.NaivePassword != "naive-password-plain" {
		t.Fatalf("NaivePassword = %q, want %q", snapshot.Settings.NaivePassword, "naive-password-plain")
	}
	if snapshot.Settings.Hysteria2Password != "hysteria2-password-plain" {
		t.Fatalf("Hysteria2Password = %q, want %q", snapshot.Settings.Hysteria2Password, "hysteria2-password-plain")
	}
	if snapshot.Warp.LicenseKey != "warp-license-plain" {
		t.Fatalf("Warp.LicenseKey = %q, want %q", snapshot.Warp.LicenseKey, "warp-license-plain")
	}
	if snapshot.Warp.PrivateKey != "warp-private-plain" {
		t.Fatalf("Warp.PrivateKey = %q, want %q", snapshot.Warp.PrivateKey, "warp-private-plain")
	}
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	cipher := newTestCipher(t)
	s := &managementState{cipher: cipher}

	// Use a richer snapshot with all 4 fields populated
	snapshot := managementSnapshot{
		Settings: Settings{
			NaivePassword:     "super-secret-naive-pass-123",
			Hysteria2Password: "hysteria2-!@#$%^&*()-pass",
		},
		Warp: WarpConfig{
			LicenseKey: "warp-license-abcdefghijklmnop",
			PrivateKey: "warp-priv-key-0123456789",
		},
	}

	// Encrypt
	s.encryptSnapshot(&snapshot)

	// Verify all encrypted
	if !secrets.IsEncrypted(snapshot.Settings.NaivePassword) {
		t.Fatal("NaivePassword not encrypted after encrypt")
	}
	if !secrets.IsEncrypted(snapshot.Settings.Hysteria2Password) {
		t.Fatal("Hysteria2Password not encrypted after encrypt")
	}
	if !secrets.IsEncrypted(snapshot.Warp.LicenseKey) {
		t.Fatal("Warp.LicenseKey not encrypted after encrypt")
	}
	if !secrets.IsEncrypted(snapshot.Warp.PrivateKey) {
		t.Fatal("Warp.PrivateKey not encrypted after encrypt")
	}

	// Decrypt
	s.decryptSnapshot(&snapshot)

	// Verify full roundtrip restores original values
	if snapshot.Settings.NaivePassword != "super-secret-naive-pass-123" {
		t.Fatalf("roundtrip NaivePassword = %q, want %q", snapshot.Settings.NaivePassword, "super-secret-naive-pass-123")
	}
	if snapshot.Settings.Hysteria2Password != "hysteria2-!@#$%^&*()-pass" {
		t.Fatalf("roundtrip Hysteria2Password = %q, want %q", snapshot.Settings.Hysteria2Password, "hysteria2-!@#$%^&*()-pass")
	}
	if snapshot.Warp.LicenseKey != "warp-license-abcdefghijklmnop" {
		t.Fatalf("roundtrip Warp.LicenseKey = %q, want %q", snapshot.Warp.LicenseKey, "warp-license-abcdefghijklmnop")
	}
	if snapshot.Warp.PrivateKey != "warp-priv-key-0123456789" {
		t.Fatalf("roundtrip Warp.PrivateKey = %q, want %q", snapshot.Warp.PrivateKey, "warp-priv-key-0123456789")
	}
}

func TestManagementStateReloadPicksUpStateChanges(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "var", "lib", "veil")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	statePath := filepath.Join(stateDir, "state.json")
	keyPath := filepath.Join(dir, "etc", "veil", "state.key")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatalf("mkdir key dir: %v", err)
	}

	// Create key and cipher
	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("load or create key: %v", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}

	// Write initial state: stack=both, domain=old.example.com
	state := &managementState{statePath: statePath, keyPath: keyPath, applyRoot: stateDir, cipher: cipher}
	state.settings = Settings{PanelListen: "127.0.0.1:2096", Stack: "both", Domain: "old.example.com"}
	if err := state.saveLocked(); err != nil {
		t.Fatalf("save initial state: %v", err)
	}

	// Verify initial state
	if state.settings.Domain != "old.example.com" {
		t.Fatalf("initial domain = %q, want old.example.com", state.settings.Domain)
	}

	// Write new state to disk (simulating SIGHUP / external modification)
	state2 := &managementState{statePath: statePath, keyPath: keyPath, applyRoot: stateDir,
		settings: Settings{PanelListen: "0.0.0.0:2096", Stack: "hysteria2", Domain: "new.example.com"}, cipher: cipher}
	if err := state2.saveLocked(); err != nil {
		t.Fatalf("save new state: %v", err)
	}

	// Reload — should pick up the new state
	if err := state.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if state.settings.Domain != "new.example.com" {
		t.Errorf("after reload domain = %q, want new.example.com", state.settings.Domain)
	}
	if state.settings.Stack != "panel" {
		t.Errorf("after reload stack = %q, want panel", state.settings.Stack)
	}
}

func TestManagementStateReloadRespectsEmptyStatePath(t *testing.T) {
	state := &managementState{statePath: "", keyPath: ""}
	// Reload with empty paths should be a no-op, not an error
	if err := state.Reload(); err != nil {
		t.Fatalf("reload with empty paths should not error: %v", err)
	}
}
