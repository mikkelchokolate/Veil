package privileged

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/safefs"
	"golang.org/x/sys/unix"
)

type fenceState struct {
	Owner       string `json:"owner"`
	Generation  uint64 `json:"generation"`
	OperationID string `json:"operationId"`
}

type fenceGuard struct {
	mu       sync.Mutex
	path     string
	required bool
	state    fenceState
}

func newFenceGuard(path string, required bool) *fenceGuard {
	return &fenceGuard{path: path, required: required}
}

func (g *fenceGuard) Accept(token FenceToken) (resultErr error) {
	if token.Owner == "" || token.Generation == 0 {
		if g.required {
			return newError(ErrorConflict, "runtime mutation requires a fencing token")
		}
		return nil
	}
	if g.required && token.OperationID == "" {
		return newError(ErrorConflict, "runtime mutation requires a fenced operation identity")
	}
	if g.required && token.LeaseExpiresAt <= time.Now().UTC().Unix() {
		return newError(ErrorConflict, "runtime mutation lease has expired")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.path == "" {
		return acceptFenceState(&g.state, token)
	}
	if err := os.MkdirAll(filepath.Dir(g.path), 0o700); err != nil {
		return err
	}
	// Pin the fence directory on a descriptor and refuse a symlinked leaf: the
	// lock path lives under the service-writable state root, so a planted
	// symlink would otherwise redirect a root-owned O_CREATE open (#1229-F5).
	fenceDir, err := safefs.OpenDir(filepath.Dir(g.path))
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, fenceDir.Close())
	}()
	lock, err := openLockFileAt(fenceDir, filepath.Base(g.path)+".lock")
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, releaseLockedFile(lock))
	}()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return err
	}
	state := fenceState{}
	body, err := os.ReadFile(g.path)
	if err == nil {
		if err := json.Unmarshal(body, &state); err != nil {
			return errors.New("invalid privileged fencing state")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := acceptFenceState(&state, token); err != nil {
		return err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(g.path, append(encoded, '\n'), 0o600, 0o700); err != nil {
		return err
	}
	g.state = state
	return nil
}

func acceptFenceState(state *fenceState, token FenceToken) error {
	if token.Generation < state.Generation {
		return newError(ErrorConflict, "stale runtime fencing generation")
	}
	if token.Generation == state.Generation && state.Owner != "" && token.Owner != state.Owner {
		return newError(ErrorConflict, "runtime fencing generation belongs to another owner")
	}
	if token.Generation == state.Generation && state.OperationID != "" && token.OperationID != state.OperationID {
		return newError(ErrorConflict, "runtime fencing generation belongs to another operation")
	}
	if token.Generation > state.Generation || state.Owner == "" {
		state.Generation = token.Generation
		state.Owner = token.Owner
		state.OperationID = token.OperationID
	}
	return nil
}
