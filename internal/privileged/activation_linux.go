//go:build linux

package privileged

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

const systemdListenFD = 3

// newSystemdUnixListener is a test hook so ServeSystemd can be exercised
// without taking over the real file descriptor 3.
var newSystemdUnixListener = systemdUnixListener

func (s *Server) ServeSystemd(ctx context.Context, allowedUID uint32, allowRoot bool) error {
	listener, err := newSystemdUnixListener()
	if err != nil {
		return err
	}
	defer listener.Close()
	return s.serveUnixListener(ctx, listener, allowedUID, allowRoot)
}

func validateSystemdListenFD(fd int) error {
	address, err := unix.Getsockname(fd)
	if err != nil {
		return fmt.Errorf("systemd helper socket fd is unavailable: %w", err)
	}
	if _, ok := address.(*unix.SockaddrUnix); !ok {
		return fmt.Errorf("systemd helper listener is not a Unix socket")
	}
	return nil
}

func systemdUnixListener() (*net.UnixListener, error) {
	pid, err := strconv.Atoi(os.Getenv("LISTEN_PID"))
	if err != nil || pid != os.Getpid() {
		return nil, fmt.Errorf("systemd LISTEN_PID does not match helper process")
	}
	fds, err := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if err != nil || fds != 1 {
		return nil, fmt.Errorf("systemd helper requires exactly one listening socket")
	}
	if err := validateSystemdListenFD(systemdListenFD); err != nil {
		return nil, err
	}
	file := os.NewFile(systemdListenFD, "veil-helper.socket")
	if file == nil {
		return nil, fmt.Errorf("systemd helper socket fd is unavailable")
	}
	listener, err := net.FileListener(file)
	_ = file.Close()
	if err != nil {
		return nil, fmt.Errorf("adopt systemd helper socket: %w", err)
	}
	unixListener, ok := listener.(*net.UnixListener)
	if !ok {
		_ = listener.Close()
		return nil, fmt.Errorf("systemd helper listener is not a Unix socket")
	}
	return unixListener, nil
}
