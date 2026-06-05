//go:build linux && linuxintegration

package privileged

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSystemdSocketActivationAdoptsFD3(t *testing.T) {
	if os.Getenv("VEIL_SYSTEMD_ACTIVATION_CHILD") == "1" {
		os.Setenv("LISTEN_PID", fmt.Sprint(os.Getpid()))
		listener, err := systemdUnixListener()
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		fmt.Println("READY")
		connection, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		defer connection.Close()
		buffer := make([]byte, 4)
		if _, err := connection.Read(buffer); err != nil || string(buffer) != "ping" {
			t.Fatalf("read ping: %v body=%q", err, buffer)
		}
		if _, err := connection.Write([]byte("pong")); err != nil {
			t.Fatal(err)
		}
		return
	}

	socketPath := filepath.Join(t.TempDir(), "activated.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	file, err := listener.File()
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	defer file.Close()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(os.Args[0], "-test.run=^TestSystemdSocketActivationAdoptsFD3$")
	command.ExtraFiles = []*os.File{file}
	command.Env = append(os.Environ(), "VEIL_SYSTEMD_ACTIVATION_CHILD=1", "LISTEN_FDS=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	ready := false
	for scanner.Scan() {
		if scanner.Text() == "READY" {
			ready = true
			break
		}
	}
	if !ready {
		_ = command.Process.Kill()
		t.Fatalf("activation child did not become ready: %q err=%v", scanner.Text(), scanner.Err())
	}
	connection, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		_ = command.Process.Kill()
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4)
	if _, err := connection.Read(buffer); err != nil || string(buffer) != "pong" {
		t.Fatalf("read pong: %v body=%q", err, buffer)
	}
	_ = connection.Close()
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
}
