package privileged

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"golang.org/x/sys/unix"
)

type fenceState struct {
	Owner      string `json:"owner"`
	Generation uint64 `json:"generation"`
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
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.path == "" {
		return acceptFenceState(&g.state, token)
	}
	if err := os.MkdirAll(filepath.Dir(g.path), 0o700); err != nil {
		return err
	}
	lockPath := g.path + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
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
	if token.Generation > state.Generation || state.Owner == "" {
		state.Generation = token.Generation
		state.Owner = token.Owner
	}
	return nil
}
