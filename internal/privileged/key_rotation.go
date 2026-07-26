package privileged

import (
	"time"

	"github.com/mikkelchokolate/Veil/internal/statecommit"
)

func rotateStateKey(statePath, keyPath string, now func() time.Time) error {
	_, err := statecommit.RotateKey(statecommit.RotateKeyOptions{
		StatePath:      statePath,
		KeyPath:        keyPath,
		TargetKeyPath:  keyPath,
		Now:            now,
		KeepSafetyCopy: true,
	})
	return err
}
