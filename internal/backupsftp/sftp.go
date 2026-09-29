package backupsftp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// RemoteFS abstracts the SFTP operations the destination logic needs so
// tests can substitute an in-memory fake instead of a live SSH server.
type RemoteFS interface {
	Stat(name string) (os.FileInfo, error)
	ReadDir(dir string) ([]os.FileInfo, error)
	Open(name string) (io.ReadCloser, error)
	Create(name string) (io.WriteCloser, error)
	ReadFile(name string) ([]byte, error)
	MkdirAll(dir string) error
	// Rename must replace an existing destination atomically (POSIX rename);
	// uploads publish through a .part name so a partial transfer never sits
	// at the final archive name.
	Rename(oldname, newname string) error
	Remove(name string) error
	Close() error
}

// Dialer connects and authenticates to the configured SFTP endpoint. It is
// the seam tests replace with an in-memory fake.
type Dialer func(ctx context.Context, config Config, knownHostsPath string) (RemoteFS, error)

// Dial connects to cfg's endpoint with a real SSH/SFTP client. It is a var
// so helper and CLI tests can inject a fake at the shared seam.
var Dial Dialer = dialSFTP

const handshakeTimeout = 30 * time.Second

func dialSFTP(ctx context.Context, config Config, knownHostsPath string) (RemoteFS, error) {
	auth, err := authMethods(config)
	if err != nil {
		return nil, err
	}
	hostKeyCallback, err := hostKeyCallback(config, knownHostsPath)
	if err != nil {
		return nil, err
	}
	address := config.Address()
	dialer := net.Dialer{Timeout: handshakeTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("sftp dial %s: %w", address, err)
	}
	// Bound the SSH handshake through the socket deadline; the context only
	// covers the TCP dial.
	deadline := time.Now().Add(handshakeTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, address, &ssh.ClientConfig{
		User:            config.User,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback,
		Timeout:         handshakeTimeout,
	})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("sftp handshake %s: %w", address, err)
	}
	_ = conn.SetDeadline(time.Time{})
	client := ssh.NewClient(sshConn, chans, reqs)
	fsClient, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("open sftp subsystem on %s: %w", address, err)
	}
	return &sftpFS{client: fsClient, ssh: client}, nil
}

func authMethods(config Config) ([]ssh.AuthMethod, error) {
	switch config.AuthType {
	case AuthTypePassword:
		if config.Password == "" {
			return nil, errors.New("sftp destination has no password configured")
		}
		return []ssh.AuthMethod{ssh.Password(config.Password)}, nil
	case AuthTypeKey:
		keyPath := strings.TrimSpace(config.KeyPath)
		if keyPath == "" {
			return nil, errors.New("sftp destination has no key path configured")
		}
		body, err := readRegularFile(keyPath, 1024*1024)
		if err != nil {
			return nil, fmt.Errorf("read sftp private key: %w", err)
		}
		var signer ssh.Signer
		if config.KeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(body, []byte(config.KeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(body)
		}
		if err != nil {
			return nil, fmt.Errorf("parse sftp private key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	default:
		return nil, fmt.Errorf("unsupported sftp authType %q", config.AuthType)
	}
}

// hostKeyCallback prefers an explicit pinned key; otherwise it does
// trust-on-first-use against the known_hosts file: an unknown host is
// recorded, a known host with a changed key is rejected.
func hostKeyCallback(config Config, knownHostsPath string) (ssh.HostKeyCallback, error) {
	if pinned := strings.TrimSpace(config.HostKey); pinned != "" {
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pinned))
		if err != nil {
			return nil, fmt.Errorf("configured sftp host key is not a valid public key: %w", err)
		}
		want := key.Marshal()
		return func(hostname string, _ net.Addr, remote ssh.PublicKey) error {
			if !bytes.Equal(remote.Marshal(), want) {
				return fmt.Errorf("sftp host key for %s does not match the configured pin", hostname)
			}
			return nil
		}, nil
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		return verifyOrTrustHostKey(knownHostsPath, hostname, remote, key)
	}, nil
}

// verifyOrTrustHostKey implements TOFU over an OpenSSH known_hosts file.
// Entries use the normalized "[host]:port" form so non-default ports are
// distinguished. A hostname already present with a different key fails
// closed; an unknown host is appended.
func verifyOrTrustHostKey(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
	callback, err := knownhosts.New(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) && !os.IsNotExist(err) {
		return fmt.Errorf("load sftp known hosts: %w", err)
	}
	if callback != nil {
		checkErr := callback(hostname, remote, key)
		if checkErr == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(checkErr, &keyErr) && len(keyErr.Want) > 0 {
			return fmt.Errorf("sftp host key for %s changed (known_hosts entry %d); refusing to continue", hostname, keyErr.Want[0].Line)
		}
		if errors.As(checkErr, &keyErr) {
			// Unknown host: fall through to trust-and-record below.
		} else {
			return checkErr
		}
	}
	return appendKnownHost(path, hostname, key)
}

func appendKnownHost(path, hostname string, key ssh.PublicKey) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("record sftp host key: %w", err)
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	if _, err := file.WriteString(line + "\n"); err != nil {
		_ = file.Close()
		return fmt.Errorf("record sftp host key: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("record sftp host key: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("record sftp host key: %w", err)
	}
	return nil
}

// sftpFS adapts *sftp.Client to RemoteFS. Remote paths always use slash
// semantics (path.Join), never filepath.Join.
type sftpFS struct {
	client *sftp.Client
	ssh    *ssh.Client
}

func (f *sftpFS) Stat(name string) (os.FileInfo, error)     { return f.client.Stat(name) }
func (f *sftpFS) ReadDir(dir string) ([]os.FileInfo, error) { return f.client.ReadDir(dir) }
func (f *sftpFS) Open(name string) (io.ReadCloser, error)   { return f.client.Open(name) }
func (f *sftpFS) ReadFile(name string) ([]byte, error) {
	file, err := f.client.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	// Callers bound this to sidecar-sized reads; the cap keeps a hostile
	// server from streaming unbounded data into memory.
	return io.ReadAll(io.LimitReader(file, 1<<20))
}
func (f *sftpFS) MkdirAll(dir string) error { return f.client.MkdirAll(dir) }
func (f *sftpFS) Remove(name string) error  { return f.client.Remove(name) }
func (f *sftpFS) Rename(oldname, newname string) error {
	// PosixRename overwrites the destination atomically where the server
	// supports the extension; fall back to plain rename (pkg/sftp removes
	// the target first) for servers without it.
	if err := f.client.PosixRename(oldname, newname); err == nil {
		return nil
	}
	return f.client.Rename(oldname, newname)
}

func (f *sftpFS) Create(name string) (io.WriteCloser, error) {
	file, err := f.client.Create(path.Clean(name))
	if err != nil {
		return nil, err
	}
	return &sftpWriteCloser{file: file}, nil
}

// sftpWriteCloser flushes an fsync at close when the server supports the
// extension; the upload path also verifies size afterwards, so a missing
// fsync capability is tolerated rather than fatal.
type sftpWriteCloser struct {
	file *sftp.File
}

func (w *sftpWriteCloser) Write(p []byte) (int, error) { return w.file.Write(p) }

func (w *sftpWriteCloser) Close() error {
	_ = w.file.Sync()
	return w.file.Close()
}

func (f *sftpFS) Close() error {
	return errors.Join(f.client.Close(), f.ssh.Close())
}
