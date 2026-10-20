//go:build !unix

package privileged

import (
	"errors"
	"fmt"
	"os"

	"github.com/mikkelchokolate/Veil/internal/safefs"
)

// errLocksUnsupported fails every privileged lock closed off-Unix: the
// helper's flock/openat contract only exists on Unix, and production installs
// run on Linux. This file exists so the CLI still compiles (#1243).
var errLocksUnsupported = fmt.Errorf("privileged file locks are unsupported on this platform: %w", errors.ErrUnsupported)

func openLockFileAt(*safefs.Dir, string) (*os.File, error) { return nil, errLocksUnsupported }

func lockFileExclusive(*os.File) error { return errLocksUnsupported }

func releaseLockedFile(file *os.File) error { return file.Close() }
