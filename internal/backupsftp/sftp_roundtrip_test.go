package backupsftp

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/mikkelchokolate/Veil/internal/backup"
)

// sftpTestServer is a real in-process SSH+SFTP endpoint: it exercises the
// production dial path (TCP, SSH handshake, host-key verification, the pkg/sftp
// protocol) without any external server.
type sftpTestServer struct {
	addr      string
	user      string
	password  string
	listener  net.Listener
	handlers  sftp.Handlers
	mu        sync.Mutex
	hostKey   ssh.Signer
	publicKey ssh.PublicKey
	serveDone chan struct{}
}

func newSFTPHostKey(t *testing.T) ssh.Signer {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func startSFTPServer(t *testing.T, user, password string) *sftpTestServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &sftpTestServer{
		addr:     listener.Addr().String(),
		user:     user,
		password: password,
		listener: listener,
		// One shared in-memory store so separate client connections see the
		// same remote filesystem.
		handlers:  sftp.InMemHandler(),
		hostKey:   newSFTPHostKey(t),
		serveDone: make(chan struct{}),
	}
	go server.serve()
	t.Cleanup(func() {
		_ = server.listener.Close()
		<-server.serveDone
	})
	return server
}

func (s *sftpTestServer) serve() {
	defer close(s.serveDone)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *sftpTestServer) handleConn(conn net.Conn) {
	s.mu.Lock()
	signer := s.hostKey
	s.mu.Unlock()
	config := &ssh.ServerConfig{
		PasswordCallback: func(meta ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if meta.User() == s.user && string(pass) == s.password {
				return nil, nil
			}
			return nil, fmt.Errorf("authentication rejected")
		},
		PublicKeyCallback: func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if meta.User() != s.user {
				return nil, fmt.Errorf("authentication rejected")
			}
			s.mu.Lock()
			ok := s.publicKey != nil && string(key.Marshal()) == string(s.publicKey.Marshal())
			s.mu.Unlock()
			if !ok {
				return nil, fmt.Errorf("authentication rejected")
			}
			return nil, nil
		},
	}
	config.AddHostKey(signer)
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		_ = conn.Close()
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)
	for channel := range chans {
		if channel.ChannelType() != "session" {
			_ = channel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		sess, requests, err := channel.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(sess, requests)
	}
}

func (s *sftpTestServer) handleSession(sess ssh.Channel, requests <-chan *ssh.Request) {
	defer sess.Close()
	for req := range requests {
		var payload struct{ Subsystem string }
		if req.Type == "subsystem" && ssh.Unmarshal(req.Payload, &payload) == nil && payload.Subsystem == "sftp" {
			_ = req.Reply(true, nil)
			_ = sftp.NewRequestServer(sess, s.handlers).Serve()
			return
		}
		_ = req.Reply(false, nil)
	}
}

// publicKey is settable for key-auth tests; nil means password-only.
func (s *sftpTestServer) setPublicKey(key ssh.PublicKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publicKey = key
}

func (s *sftpTestServer) setHostKey(signer ssh.Signer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hostKey = signer
}

func (s *sftpTestServer) pinnedHostKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(s.hostKey.PublicKey())))
}

func TestSftpRealRoundtripUploadListFetchPrune(t *testing.T) {
	server := startSFTPServer(t, "veil", "pw-secret")
	dir := t.TempDir()
	config := sftpTestConfig()
	config.User = "veil"
	config.Password = "pw-secret"
	config.Host = "127.0.0.1"
	port := server.addr[strings.LastIndex(server.addr, ":")+1:]
	config.Port = mustAtoi(t, port)
	config.HostKey = server.pinnedHostKey()
	config.RemoteDir = "/srv/veil-backups"

	remote, err := Dial(context.Background(), config, filepath.Join(dir, KnownHostsFileName))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	localPath := writeLocalArchive(t, []byte("real-encrypted-archive"))
	want := localArchiveContent(t, localPath)
	name := filepath.Base(localPath)
	receipt, err := Upload(context.Background(), remote, config, localPath, name)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if receipt.Size != int64(len(want)) {
		t.Fatalf("receipt=%+v", receipt)
	}

	entries, err := List(context.Background(), remote, config)
	if err != nil || len(entries) != 1 || entries[0].Name != name {
		t.Fatalf("list = %+v, %v", entries, err)
	}

	fetched, err := Fetch(context.Background(), remote, config, dir, name)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	body, err := os.ReadFile(fetched.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(want) {
		t.Fatalf("fetched=%q", body)
	}

	// Retention over the real transport: two old archives prune under daily=0.
	if err := remote.MkdirAll(config.RemoteDir); err != nil {
		t.Fatal(err)
	}
	old := "veil_backup_20250101_020000.tar.gz.enc"
	w, err := remote.Create(config.RemoteDir + "/" + old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	pruned, err := Prune(context.Background(), remote, config, backup.RetentionPolicy{})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	for _, deleted := range pruned.Deleted {
		if deleted == old {
			return
		}
	}
	t.Fatalf("old archive not pruned: %+v", pruned.Deleted)
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSftpDialRejectsWrongPassword(t *testing.T) {
	server := startSFTPServer(t, "veil", "pw-secret")
	config := sftpTestConfig()
	config.User = "veil"
	config.Password = "wrong"
	config.Host = "127.0.0.1"
	config.Port = mustAtoi(t, server.addr[strings.LastIndex(server.addr, ":")+1:])
	config.HostKey = server.pinnedHostKey()
	if _, err := Dial(context.Background(), config, filepath.Join(t.TempDir(), KnownHostsFileName)); err == nil {
		t.Fatal("wrong password authenticated")
	}
}

func TestSftpDialTOFURecordsAndRejectsChangedKey(t *testing.T) {
	server := startSFTPServer(t, "veil", "pw-secret")
	knownHosts := filepath.Join(t.TempDir(), KnownHostsFileName)
	config := sftpTestConfig()
	config.User = "veil"
	config.Password = "pw-secret"
	config.Host = "127.0.0.1"
	config.Port = mustAtoi(t, server.addr[strings.LastIndex(server.addr, ":")+1:])
	config.HostKey = "" // trust-on-first-use

	first, err := Dial(context.Background(), config, knownHosts)
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	_ = first.Close()
	body, err := os.ReadFile(knownHosts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "ssh-ed25519") {
		t.Fatalf("known_hosts=%q", body)
	}
	info, err := os.Stat(knownHosts)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("known_hosts mode = %o, want 0600", info.Mode().Perm())
	}

	// Same key still verifies.
	second, err := Dial(context.Background(), config, knownHosts)
	if err != nil {
		t.Fatalf("second dial: %v", err)
	}
	_ = second.Close()

	// A different host key at the same address must be rejected.
	server.setHostKey(newSFTPHostKey(t))
	if _, err := Dial(context.Background(), config, knownHosts); err == nil ||
		!strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed host key must fail, got %v", err)
	}
}

func TestSftpDialPinnedKeyMismatchFails(t *testing.T) {
	server := startSFTPServer(t, "veil", "pw-secret")
	config := sftpTestConfig()
	config.User = "veil"
	config.Password = "pw-secret"
	config.Host = "127.0.0.1"
	config.Port = mustAtoi(t, server.addr[strings.LastIndex(server.addr, ":")+1:])
	config.HostKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(newSFTPHostKey(t).PublicKey())))
	if _, err := Dial(context.Background(), config, filepath.Join(t.TempDir(), KnownHostsFileName)); err == nil ||
		!strings.Contains(err.Error(), "pin") {
		t.Fatalf("pinned-key mismatch must fail, got %v", err)
	}
}

// #1229-F6: the TOFU append must not follow a symlinked known_hosts — the
// state dir sits where a lower-privileged account can plant dentries, and the
// append runs as root.
func TestAppendKnownHostRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sentinel")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	knownHosts := filepath.Join(dir, KnownHostsFileName)
	if err := os.Symlink(target, knownHosts); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	err := appendKnownHost(knownHosts, "backups.example.com:22", newSFTPHostKey(t).PublicKey())
	if err == nil {
		t.Fatal("append through a symlinked known_hosts succeeded")
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != "keep" {
		t.Fatalf("symlink target modified: %q, %v", body, err)
	}
	if linked, err := os.ReadFile(knownHosts); err == nil && string(linked) != "keep" {
		t.Fatal("append wrote through the symlink")
	}
}

// #1202: the stall guard keeps the handshake's absolute deadline untouched,
// then switches to per-I/O deadlines once it is cleared — an idle socket must
// time out on its own instead of wedging a transfer forever.
func TestStallGuardConnDeadlineModes(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	guarded := &stallGuardConn{Conn: c1, idle: 80 * time.Millisecond}

	// Handshake mode: an absolute SetDeadline bounds the read directly.
	_ = guarded.SetDeadline(time.Now().Add(60 * time.Millisecond))
	if _, err := guarded.Read(make([]byte, 1)); err == nil {
		t.Fatal("handshake-phase read did not observe the absolute deadline")
	}

	// Post-handshake: clearing the deadline arms the stall guard — the next
	// stalled read must fail near the idle bound, not at the (already past)
	// handshake deadline and not never.
	_ = guarded.SetDeadline(time.Time{})
	start := time.Now()
	if _, err := guarded.Read(make([]byte, 1)); err == nil {
		t.Fatal("stalled read never timed out")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("stalled read took %v to fail", elapsed)
	}
}

func TestSftpDialKeyAuth(t *testing.T) {
	server := startSFTPServer(t, "veil", "pw-secret")
	_, clientKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(clientKey)
	if err != nil {
		t.Fatal(err)
	}
	server.setPublicKey(signer.PublicKey())

	// Write the client private key in OpenSSH PEM form for authMethods. The
	// dial path confines key files to the managed etc dir (#1229), so point
	// VEIL_ETC_DIR at a tempdir and keep the key inside it.
	etcDir := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", etcDir)
	pemBlock, err := ssh.MarshalPrivateKey(clientKey, "test")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(etcDir, "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(pemBlock), 0o600); err != nil {
		t.Fatal(err)
	}

	config := sftpTestConfig()
	config.User = "veil"
	config.AuthType = AuthTypeKey
	config.KeyPath = filepath.ToSlash(keyPath)
	config.Password = ""
	config.Host = "127.0.0.1"
	config.Port = mustAtoi(t, server.addr[strings.LastIndex(server.addr, ":")+1:])
	config.HostKey = server.pinnedHostKey()

	remote, err := Dial(context.Background(), config, filepath.Join(t.TempDir(), KnownHostsFileName))
	if err != nil {
		t.Fatalf("key-auth dial: %v", err)
	}
	_ = remote.Close()
}
