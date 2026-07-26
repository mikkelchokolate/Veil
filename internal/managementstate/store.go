package managementstate

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

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
	if err := s.decryptSnapshot(&snapshot); err != nil {
		return model.ManagementSnapshot{}, false, err
	}
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
	return s.SaveEncoded(body)
}

// SaveEncoded atomically publishes an already encoded Management snapshot.
// It preserves the ownership and permissions of an existing state file.
func (s Store) SaveEncoded(body []byte) error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	// Preserve ownership and permissions of an existing state file so that
	// CLI commands (e.g. `veil admin reset`) do not lock out the veil user.
	var prev *fileInfo
	if fi, err := os.Stat(s.path); err == nil {
		prev = &fileInfo{uid: fileOwnerUID(fi), gid: fileOwnerGID(fi), mode: fi.Mode().Perm()}
	}

	return writeStoreFileAtomic(s.path, body, prev)
}

// RestoreEncoded restores the exact bytes that existed before a failed state
// publication. When existed is false it removes the newly-created state file.
func (s Store) RestoreEncoded(body []byte, existed bool) error {
	if s.path == "" {
		return nil
	}
	if existed {
		return s.SaveEncoded(body)
	}
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	bestEffortSyncStoreDirectory(filepath.Dir(s.path))
	return nil
}

type fileInfo struct {
	uid  int
	gid  int
	mode os.FileMode
}

func fileOwnerUID(fi os.FileInfo) int {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid)
	}
	return -1
}

func fileOwnerGID(fi os.FileInfo) int {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(st.Gid)
	}
	return -1
}

func (s Store) Marshal(snapshot model.ManagementSnapshot) ([]byte, error) {
	// Work on a deep copy so encryption does not mutate the caller's snapshot.
	snapshot = BuildSnapshot(SnapshotInput{
		Setup:         snapshot.Setup,
		Settings:      snapshot.Settings,
		Inbounds:      snapshot.Inbounds,
		Rules:         snapshot.Rules,
		RoutingPreset: snapshot.RoutingPreset,
		RoutingSource: snapshot.RoutingSource,
		Warp:          snapshot.Warp,
		Users:         snapshot.Users,
	})
	if err := s.encryptSnapshot(&snapshot); err != nil {
		return nil, err
	}
	return NewManagementStateCodec().Encode(snapshot)
}

func (s Store) encryptSnapshot(snapshot *model.ManagementSnapshot) error {
	if s.cipher == nil {
		return nil
	}
	encrypt := func(v string) (string, error) {
		if v == "" || secrets.IsEncrypted(v) {
			return v, nil
		}
		return s.cipher.Encrypt(v)
	}
	return NewSecretPolicy().Transform(snapshot, encrypt)
}

func (s Store) decryptSnapshot(snapshot *model.ManagementSnapshot) error {
	if s.cipher == nil {
		return nil
	}
	decrypt := func(v string) (string, error) {
		if v == "" {
			return v, nil
		}
		if !secrets.IsEncrypted(v) {
			return v, nil
		}
		return s.cipher.Decrypt(v)
	}
	return NewSecretPolicy().Transform(snapshot, decrypt)
}

func EncryptSnapshot(snapshot *model.ManagementSnapshot, cipher *secrets.Cipher) error {
	return NewStore("", cipher).encryptSnapshot(snapshot)
}

func DecryptSnapshot(snapshot *model.ManagementSnapshot, cipher *secrets.Cipher) error {
	return NewStore("", cipher).decryptSnapshot(snapshot)
}
