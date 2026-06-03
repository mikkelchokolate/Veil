package managementstate

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

type Store struct {
	path   string
	cipher *secrets.Cipher
}

type StateStore = Store

func NewStore(path string, cipher *secrets.Cipher) Store {
	return Store{path: path, cipher: cipher}
}

func NewStateStore(path string, cipher *secrets.Cipher) StateStore {
	return NewStore(path, cipher)
}

func (s Store) Load() (model.ManagementSnapshot, bool, error) {
	if s.path == "" {
		return model.ManagementSnapshot{}, false, nil
	}
	body, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.ManagementSnapshot{}, false, nil
		}
		return model.ManagementSnapshot{}, false, err
	}
	snapshot, err := NewManagementStateCodec().Decode(body)
	if err != nil {
		return model.ManagementSnapshot{}, false, err
	}
	s.decryptSnapshot(&snapshot)
	return snapshot, true, nil
}

func (s Store) Save(snapshot model.ManagementSnapshot) error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	body, err := s.Marshal(snapshot)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s Store) Marshal(snapshot model.ManagementSnapshot) ([]byte, error) {
	s.encryptSnapshot(&snapshot)
	return NewManagementStateCodec().Encode(snapshot)
}

func (s Store) encryptSnapshot(snapshot *model.ManagementSnapshot) {
	if s.cipher == nil {
		return
	}
	encrypt := func(v string) string {
		if v == "" || secrets.IsEncrypted(v) {
			return v
		}
		if enc, err := s.cipher.Encrypt(v); err == nil {
			return enc
		}
		return v
	}
	SecretPolicy{}.Transform(snapshot, encrypt)
}

func (s Store) decryptSnapshot(snapshot *model.ManagementSnapshot) {
	if s.cipher == nil {
		return
	}
	decrypt := func(v string) string {
		if v == "" {
			return v
		}
		if dec, err := s.cipher.Decrypt(v); err == nil {
			return dec
		}
		return v
	}
	SecretPolicy{}.Transform(snapshot, decrypt)
}

func EncryptSnapshot(snapshot *model.ManagementSnapshot, cipher *secrets.Cipher) {
	NewStore("", cipher).encryptSnapshot(snapshot)
}

func DecryptSnapshot(snapshot *model.ManagementSnapshot, cipher *secrets.Cipher) {
	NewStore("", cipher).decryptSnapshot(snapshot)
}
