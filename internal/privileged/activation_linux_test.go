//go:build linux

package privileged

import (
	"fmt"
	"os"
	"testing"
)

func TestSystemdUnixListenerRejectsInvalidEnvironment(t *testing.T) {
	t.Run("missing pid", func(t *testing.T) {
		t.Setenv("LISTEN_PID", "")
		t.Setenv("LISTEN_FDS", "1")
		if _, err := systemdUnixListener(); err == nil {
			t.Fatal("expected missing LISTEN_PID to fail")
		}
	})
	t.Run("wrong pid", func(t *testing.T) {
		t.Setenv("LISTEN_PID", fmt.Sprint(os.Getpid()+1))
		t.Setenv("LISTEN_FDS", "1")
		if _, err := systemdUnixListener(); err == nil {
			t.Fatal("expected mismatched LISTEN_PID to fail")
		}
	})
	t.Run("missing fds", func(t *testing.T) {
		t.Setenv("LISTEN_PID", fmt.Sprint(os.Getpid()))
		t.Setenv("LISTEN_FDS", "")
		if _, err := systemdUnixListener(); err == nil {
			t.Fatal("expected missing LISTEN_FDS to fail")
		}
	})
	t.Run("wrong fd count", func(t *testing.T) {
		t.Setenv("LISTEN_PID", fmt.Sprint(os.Getpid()))
		t.Setenv("LISTEN_FDS", "2")
		if _, err := systemdUnixListener(); err == nil {
			t.Fatal("expected LISTEN_FDS != 1 to fail")
		}
	})
	t.Run("fd unavailable", func(t *testing.T) {
		t.Setenv("LISTEN_PID", fmt.Sprint(os.Getpid()))
		t.Setenv("LISTEN_FDS", "1")
		if _, err := systemdUnixListener(); err == nil {
			t.Fatal("expected unavailable fd 3 to fail")
		}
	})
}

func TestValidateSystemdListenFDDoesNotCloseNonSocket(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "not-a-socket")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := validateSystemdListenFD(int(file.Fd())); err == nil {
		t.Fatal("ordinary file accepted as systemd socket")
	}
	if _, err := file.WriteString("still-open"); err != nil {
		t.Fatalf("validation closed unrelated descriptor: %v", err)
	}
}
