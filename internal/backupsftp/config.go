// Package backupsftp implements the SFTP remote-backup destination: config
// and secrets persisted in a root-only file under the etc dir, the SSH/SFTP
// client, upload with checksum verification, remote listing/download, and a
// remote retention prune that shares the policy decision with the local
// archive prune in internal/backup.
//
// Secret fields (password, key passphrase, pinned host key) live only in the
// on-disk config file. Anything returned over the API goes through View,
// which reports "configured" flags instead of the values.
package backupsftp

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

const (
	// ConfigFileName is the root-only destination config under the etc dir.
	ConfigFileName = "backup-sftp.json"
	// StatusFileName records the last remote operation outcome under the
	// state dir (the config dir itself is read-only for the scheduled unit).
	StatusFileName = "backup-sftp-status.json"
	// KnownHostsFileName holds the trust-on-first-use remote host keys.
	KnownHostsFileName = "backup-sftp.known_hosts"

	AuthTypeKey      = "key"
	AuthTypePassword = "password"

	DefaultPort = 22

	MaxPasswordBytes    = 4096
	MaxKeyPassphraseBts = 4096
	maxConfigBytes      = 64 * 1024
)

// Config is the SFTP destination as stored on disk. Password and
// KeyPassphrase are secrets: they are write-only over the API and must never
// be echoed back or stored in the management state DB.
type Config struct {
	Enabled       bool   `json:"enabled"`
	Host          string `json:"host"`
	Port          int    `json:"port,omitempty"`
	User          string `json:"user"`
	RemoteDir     string `json:"remoteDir"`
	AuthType      string `json:"authType"`
	KeyPath       string `json:"keyPath,omitempty"`
	KeyPassphrase string `json:"keyPassphrase,omitempty"`
	Password      string `json:"password,omitempty"`
	// HostKey optionally pins the server host public key in authorized_keys
	// format ("ssh-ed25519 AAAA…"). When empty the first observed key is
	// trusted and persisted in the known_hosts file (TOFU).
	HostKey string `json:"hostKey,omitempty"`
}

// View is the API-safe rendering of Config: secrets are replaced by
// "configured" flags so a GET can never echo them.
type View struct {
	Configured       bool   `json:"configured"`
	Enabled          bool   `json:"enabled"`
	Host             string `json:"host,omitempty"`
	Port             int    `json:"port,omitempty"`
	User             string `json:"user,omitempty"`
	RemoteDir        string `json:"remoteDir,omitempty"`
	AuthType         string `json:"authType,omitempty"`
	KeyPath          string `json:"keyPath,omitempty"`
	PasswordSet      bool   `json:"passwordSet,omitempty"`
	KeyPassphraseSet bool   `json:"keyPassphraseSet,omitempty"`
	HostKeySet       bool   `json:"hostKeySet,omitempty"`
}

// Status records the outcome of the most recent remote operation attempts.
// It lives under the state dir so both the helper and the scheduled backup
// oneshot (which mounts only /var/lib/veil writable) can update it.
type Status struct {
	LastUploadAt      string `json:"lastUploadAt,omitempty"`
	LastUploadArchive string `json:"lastUploadArchive,omitempty"`
	LastFetchAt       string `json:"lastFetchAt,omitempty"`
	LastFetchArchive  string `json:"lastFetchArchive,omitempty"`
	LastPruneAt       string `json:"lastPruneAt,omitempty"`
	LastError         string `json:"lastError,omitempty"`
	LastErrorAt       string `json:"lastErrorAt,omitempty"`
}

// Validate checks a destination config before it is persisted. Address is
// the TCP dial address.
func (c Config) Validate() error {
	host := strings.TrimSpace(c.Host)
	if host == "" {
		return errors.New("destination host is required")
	}
	if len(host) > 253 || strings.ContainsAny(host, " \t\r\n/\\@") {
		return errors.New("destination host must be a hostname or IP without whitespace")
	}
	if c.Port < 0 || c.Port > 65535 {
		return errors.New("destination port must be between 1 and 65535")
	}
	user := strings.TrimSpace(c.User)
	if user == "" {
		return errors.New("destination user is required")
	}
	if strings.ContainsAny(user, "\x00\r\n") {
		return errors.New("destination user contains invalid characters")
	}
	dir := strings.TrimSpace(c.RemoteDir)
	if dir == "" {
		return errors.New("remote directory is required")
	}
	if strings.ContainsAny(dir, "\x00\r\n") {
		return errors.New("remote directory contains invalid characters")
	}
	switch c.AuthType {
	case AuthTypeKey:
		keyPath := strings.TrimSpace(c.KeyPath)
		if keyPath == "" {
			return errors.New("key-file authentication requires a key path")
		}
		if !filepath.IsAbs(keyPath) && !strings.HasPrefix(filepath.ToSlash(keyPath), "/") {
			return errors.New("SFTP key path must be absolute")
		}
		// The helper and the scheduled backup unit run with ProtectHome=yes,
		// so a key hidden under /root, /home, or /run/user would be unreadable
		// at upload time (the same constraint backup passphrase paths obey).
		slash := filepath.ToSlash(filepath.Clean(keyPath))
		for _, prefix := range []string{"/root", "/home", "/run/user"} {
			if slash == prefix || strings.HasPrefix(slash, prefix+"/") {
				return fmt.Errorf("SFTP key path %s is hidden by ProtectHome=yes; store it under the etc dir", keyPath)
			}
		}
		if len(c.KeyPassphrase) > MaxKeyPassphraseBts {
			return errors.New("key passphrase exceeds the size limit")
		}
	case AuthTypePassword:
		if len(c.Password) > MaxPasswordBytes {
			return errors.New("password exceeds the size limit")
		}
		if c.Password == "" {
			return errors.New("password authentication requires a password")
		}
	default:
		return errors.New("authType must be \"key\" or \"password\"")
	}
	if strings.ContainsAny(c.HostKey, "\x00\r") {
		return errors.New("host key contains invalid characters")
	}
	return nil
}

// Address returns the host:port dial address with the default port applied.
func (c Config) Address() string {
	port := c.Port
	if port == 0 {
		port = DefaultPort
	}
	return net.JoinHostPort(c.Host, strconv.Itoa(port))
}

// PublicView renders the secret-free view. PasswordSet/KeyPassphraseSet/
// HostKeySet tell the UI a value is on file without revealing it.
func (c Config) PublicView() View {
	return View{
		Configured:       true,
		Enabled:          c.Enabled,
		Host:             c.Host,
		Port:             c.Port,
		User:             c.User,
		RemoteDir:        c.RemoteDir,
		AuthType:         c.AuthType,
		KeyPath:          c.KeyPath,
		PasswordSet:      c.Password != "",
		KeyPassphraseSet: c.KeyPassphrase != "",
		HostKeySet:       c.HostKey != "",
	}
}

// LoadConfig reads the destination config. A missing file is not an error:
// it returns a nil config so callers can report "not configured".
func LoadConfig(path string) (*Config, error) {
	body, err := readRegularFile(path, maxConfigBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	config, err := unmarshalConfig(body)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return config, nil
}

// SaveConfig writes the destination config atomically with root-only
// permissions — the file carries secrets and must never be world-readable.
// A zero port normalizes to the SSH default before persisting.
func SaveConfig(path string, config Config) error {
	if config.Port == 0 {
		config.Port = DefaultPort
	}
	if err := config.Validate(); err != nil {
		return err
	}
	body, err := marshalConfig(config)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, body, 0o600, 0o700)
}

// DeleteConfig removes the destination config; a missing file is a no-op.
func DeleteConfig(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove SFTP destination config: %w", err)
	}
	return nil
}

// LoadStatus reads the last remote-operation status; missing is empty.
func LoadStatus(path string) Status {
	status := Status{}
	body, err := readRegularFile(path, maxConfigBytes)
	if err != nil {
		return status
	}
	_ = unmarshalStatus(body, &status)
	return status
}

// SaveStatus persists the status file atomically (root-only; it mentions
// archive names and error text, not secrets).
func SaveStatus(path string, status Status) error {
	body, err := marshalStatus(status)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, body, 0o600, 0o700)
}

// readRegularFile is a test-overridable read so error paths are exercisable.
var readRegularFile = func(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s exceeds the %d-byte limit", path, limit)
	}
	return body, nil
}
