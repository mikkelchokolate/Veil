package managementstate

import (
	"crypto/rand"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

// #1098: the sentinel credential rendered for a revoked inbound must be keyed
// by a per-install secret so it is unguessable even when every credential
// field is empty. The store injects that secret (derived from the state key)
// into settings on load, and settings mutations must never strip it.

func newCredentialDerivationCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	var key [secrets.KeySize]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	return cipher
}

func TestStoreLoadInjectsCredentialDerivationSecret(t *testing.T) {
	cipher := newCredentialDerivationCipher(t)
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path, cipher)

	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"},
	}
	if err := store.Save(snapshot); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, ok, err := store.Load()
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	want := secrets.DeriveToken(cipher, model.CredentialDerivationLabel)
	if loaded.Settings.CredentialDerivationSecret != want {
		t.Fatalf("CredentialDerivationSecret = %q, want derived token", loaded.Settings.CredentialDerivationSecret)
	}
}

func TestCredentialDerivationSecretNeverSerialized(t *testing.T) {
	cipher := newCredentialDerivationCipher(t)
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(path, cipher)

	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{
			PanelListen:                "127.0.0.1:2096",
			Mode:                       "dev",
			CredentialDerivationSecret: "must-not-persist",
		},
	}
	body, err := store.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "must-not-persist") || strings.Contains(string(body), "redentialDerivationSecret") {
		t.Fatalf("runtime-only credential secret serialized into state:\n%s", string(body))
	}
}

func TestUpdateSettingsPreservesCredentialDerivationSecret(t *testing.T) {
	settings := Settings{
		PanelListen:                "127.0.0.1:2096",
		Mode:                       "dev",
		CredentialDerivationSecret: "install-secret",
	}
	mutation := NewManagementStateMutation(ManagementStateMutationTarget{Settings: &settings}, func() error { return nil })

	// A PUT-style update decoded from a client body can never carry the
	// runtime-only field; the mutation must preserve the injected value.
	if _, err := mutation.UpdateSettings(Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", Domain: "new.example"}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if settings.CredentialDerivationSecret != "install-secret" {
		t.Fatalf("CredentialDerivationSecret stripped by settings update: %q", settings.CredentialDerivationSecret)
	}
}
