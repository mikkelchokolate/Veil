package privileged

import (
	"crypto/rand"
	"fmt"
	"os"
	"time"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

func rotateStateKey(statePath, keyPath string, now func() time.Time) error {
	oldKeyBody, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("read old key: %w", err)
	}
	if len(oldKeyBody) != secrets.KeySize {
		return fmt.Errorf("old key has wrong length: %d", len(oldKeyBody))
	}
	var oldKey [secrets.KeySize]byte
	copy(oldKey[:], oldKeyBody)
	oldCipher, err := secrets.NewCipher(oldKey)
	if err != nil {
		return err
	}
	snapshot, ok, err := managementstate.NewStore(statePath, oldCipher).Load()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no state found at %s", statePath)
	}

	var newKey [secrets.KeySize]byte
	if _, err := rand.Read(newKey[:]); err != nil {
		return err
	}
	newCipher, err := secrets.NewCipher(newKey)
	if err != nil {
		return err
	}
	stateBody, err := managementstate.NewStore(statePath, newCipher).Marshal(snapshot)
	if err != nil {
		return err
	}
	suffix := now().UTC().Format("20060102T150405.000000000Z")
	stateSafety := statePath + ".pre-rotation-" + suffix
	keySafety := keyPath + ".pre-rotation-" + suffix
	if err := os.Rename(statePath, stateSafety); err != nil {
		return fmt.Errorf("backup state: %w", err)
	}
	if err := os.Rename(keyPath, keySafety); err != nil {
		_ = os.Rename(stateSafety, statePath)
		return fmt.Errorf("backup key: %w", err)
	}
	if err := os.WriteFile(keyPath, newKey[:], 0o600); err != nil {
		_ = os.Rename(keySafety, keyPath)
		_ = os.Rename(stateSafety, statePath)
		return fmt.Errorf("write new key: %w", err)
	}
	if err := os.WriteFile(statePath, stateBody, 0o600); err != nil {
		_ = os.Remove(keyPath)
		_ = os.Rename(keySafety, keyPath)
		_ = os.Rename(stateSafety, statePath)
		return fmt.Errorf("write re-encrypted state: %w", err)
	}
	return nil
}
