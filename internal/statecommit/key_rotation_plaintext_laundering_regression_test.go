package statecommit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

// #1283 follow-up: RotateKey must not re-seal a planted non-"ve1:" value into
// a valid ciphertext under the new key. Both the retained snapshot credential
// path (consumed by RevealEncrypted on the public /s/ feed) and the durable
// idempotency replay envelopes are covered; the rotation must abort and leave
// the old key in place.

func assertRotateKeyRefusedAndKeyKept(t *testing.T, fixture keyRotationFixture) {
	t.Helper()
	_, err := RotateKey(RotateKeyOptions{
		StatePath: fixture.statePath, KeyPath: fixture.keyPath, DatabasePath: fixture.databasePath,
	})
	if err == nil {
		t.Fatal("RotateKey re-sealed a planted plaintext value under the new key")
	}
	if !strings.Contains(err.Error(), "ciphertext is invalid") {
		t.Fatalf("RotateKey failed for an unrelated reason: %v", err)
	}
	key, err := os.ReadFile(fixture.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key, fixture.oldKey) {
		t.Fatal("refused rotation still replaced the master key")
	}
}

func TestRotateKeyRefusesPlantedPlaintextSnapshotCredential(t *testing.T) {
	fixture := newKeyRotationFixture(t)
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	repository := client.NewRepository(db)
	clientRow, err := repository.Create(client.Client{Name: "victim", Enabled: true, QuotaResetPolicy: client.ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repository.CreateBinding(client.Binding{ClientID: clientRow.ID, InboundID: "hy2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	planted := model.ManagementSnapshot{
		Settings: model.Settings{Mode: "dev", Domain: "before.example.com"},
		Credentials: []model.CredentialSnapshot{{
			ID: "planted-credential", BindingID: binding.ID, Kind: "password",
			EncryptedValue: []byte("attacker-chosen-password"), KeyVersion: 1, CredentialVersion: 1,
		}},
	}
	payload, err := json.Marshal(planted)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(fixture.oldState)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	revision, err := apply.BumpDesiredTx(tx)
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := apply.SaveSnapshotTxBound(tx, revision, payload, hex.EncodeToString(digest[:])); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	assertRotateKeyRefusedAndKeyKept(t, fixture)
}

func TestRotateKeyRefusesPlantedPlaintextReplayEnvelope(t *testing.T) {
	fixture := newKeyRotationFixture(t)
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO idempotency_results
	  (id,scope,operation_generation,response_status,response_headers,response_body,encrypted,created_at,expires_at)
	  VALUES(?,?,?,?,?,?,?,?,?)`,
		"planted-1", "scope-1", 1, 200, []byte(`{}`), []byte(`{"actor":"alice","body":"c2VjcmV0"}`), 1, 1, 9_999_999_999); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	assertRotateKeyRefusedAndKeyKept(t, fixture)
}
