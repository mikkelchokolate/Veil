package statecommit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestRotateKeyReencryptsHistoricSnapshotsForRollback(t *testing.T) {
	fixture := newKeyRotationFixture(t)
	oldCipher := cipherFromBytes(t, fixture.oldKey)
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	repository := client.NewRepository(db)
	clientRow, err := repository.Create(client.Client{Name: "kept", Enabled: true, QuotaResetPolicy: client.ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repository.CreateBinding(client.Binding{ClientID: clientRow.ID, InboundID: "hy2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := client.NewCredentialStore(db, oldCipher).Set(binding.ID, "password", "normalized-secret")
	if err != nil {
		t.Fatal(err)
	}
	historic := model.ManagementSnapshot{
		Settings: model.Settings{Mode: "dev", Domain: "before.example.com", NaivePassword: "panel-secret"},
		Inbounds: []model.Inbound{{
			Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true, Password: "vip-secret",
		}},
		Credentials: []model.CredentialSnapshot{{
			ID: credential.ID, BindingID: binding.ID, Kind: "password",
			EncryptedValue: credential.EncryptedValue, KeyVersion: credential.KeyVersion,
			CredentialVersion: credential.CredentialVersion,
		}},
	}
	if err := managementstate.EncryptSnapshot(&historic, oldCipher); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(historic)
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

	if _, err := RotateKey(RotateKeyOptions{
		StatePath: fixture.statePath, KeyPath: fixture.keyPath, DatabasePath: fixture.databasePath,
	}); err != nil {
		t.Fatal(err)
	}
	newKey, err := os.ReadFile(fixture.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	newCipher := cipherFromBytes(t, newKey)
	db, err = storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	loaded, err := apply.NewSnapshotStore(db).Load(revision)
	if err != nil {
		t.Fatal(err)
	}
	var restored model.ManagementSnapshot
	if err := json.Unmarshal(loaded, &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.Inbounds) != 1 || !secrets.IsEncrypted(restored.Inbounds[0].Password) {
		t.Fatalf("historic inbound password not encrypted: %+v", restored.Inbounds)
	}
	if _, err := oldCipher.Decrypt(restored.Inbounds[0].Password); err == nil {
		t.Fatal("historic inbound password remained decryptable with previous key")
	}
	if err := managementstate.DecryptSnapshot(&restored, newCipher); err != nil {
		t.Fatalf("historic snapshot not decryptable with rotated key: %v", err)
	}
	if restored.Settings.NaivePassword != "panel-secret" || restored.Inbounds[0].Password != "vip-secret" {
		t.Fatalf("historic secrets=%q/%q", restored.Settings.NaivePassword, restored.Inbounds[0].Password)
	}
	if len(restored.Credentials) != 1 {
		t.Fatalf("historic credentials=%+v", restored.Credentials)
	}
	plain, err := newCipher.Decrypt(string(restored.Credentials[0].EncryptedValue))
	if err != nil || plain != "normalized-secret" {
		t.Fatalf("historic credential with new key: plain=%q err=%v", plain, err)
	}
	if _, err := oldCipher.Decrypt(string(restored.Credentials[0].EncryptedValue)); err == nil {
		t.Fatal("historic snapshot credential remained decryptable with previous key")
	}
}
