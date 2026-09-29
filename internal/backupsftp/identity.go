// Per-installation remote namespace for the SFTP backup destination.
//
// Several Veil nodes may legitimately point at the same SFTP server and
// remoteDir — one backup box is the natural operator setup. Because archive
// names carry no host identity, a node must never trust the shared listing:
// its retention would otherwise prune archives belonging to other nodes
// (#1184). Every remote operation therefore happens under
// <remoteDir>/veil-node-<install-id>/ where <install-id> is a random 128-bit
// identity persisted locally the first time the node talks to a remote.
//
// The id file lives under the state dir (beside the status and known_hosts
// files) so the scheduled backup unit — which mounts only /var/lib/veil
// writable — can mint it. It is not a secret and is never sent anywhere
// except as the remote directory name. Archives another node uploaded into
// its own namespace, and archives written by older Veil versions at the
// remoteDir root, stay invisible to List/Fetch and untouchable by Prune.
package backupsftp

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// remoteNamespacePrefix makes per-node directories self-describing to an
	// operator browsing the SFTP server.
	remoteNamespacePrefix = "veil-node-"

	installIDBytes    = 16
	maxInstallIDBytes = 128
)

// installIDPattern is the only accepted install-id shape — a fixed-length
// lowercase hex token, which is also a safe single POSIX path segment.
var installIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// namespaceDir returns the remote subdirectory name for installID.
func namespaceDir(installID string) string {
	return remoteNamespacePrefix + installID
}

// loadOrCreateInstallID returns this installation's stable remote-namespace
// identity, minting and persisting one on first use. A present but malformed
// file fails closed rather than silently minting a second identity (which
// would strand the archives uploaded under the first). The create path uses
// O_EXCL so two processes racing a first run converge on the same id instead
// of last-writer-wins.
func loadOrCreateInstallID(idPath string) (string, error) {
	for attempt := 0; attempt < 4; attempt++ {
		body, err := readRegularFile(idPath, maxInstallIDBytes)
		if err == nil {
			id := strings.TrimSpace(string(body))
			if !installIDPattern.MatchString(id) {
				return "", fmt.Errorf("sftp install id %s is malformed; remove it to mint a new remote namespace", filepath.Base(idPath))
			}
			return id, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read sftp install id: %w", err)
		}
		id, err := createInstallID(idPath)
		if errors.Is(err, os.ErrExist) {
			// A concurrent first run won the create; re-read its id.
			continue
		}
		if err != nil {
			return "", err
		}
		return id, nil
	}
	return "", fmt.Errorf("could not establish sftp install id at %s", filepath.Base(idPath))
}

// createInstallID writes a freshly generated id with create-exclusive
// semantics, returning os.ErrExist when the file already exists. The file is
// written in place (not rename-published) because exclusivity is the point —
// and a crashed writer is cleaned up rather than left as a half token.
func createInstallID(idPath string) (string, error) {
	var raw [installIDBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate sftp install id: %w", err)
	}
	if dir := filepath.Dir(idPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("create sftp install id directory: %w", err)
		}
	}
	file, err := os.OpenFile(idPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	id := hex.EncodeToString(raw[:])
	fail := func(err error) (string, error) {
		_ = file.Close()
		_ = os.Remove(idPath)
		return "", err
	}
	if _, err := file.WriteString(id + "\n"); err != nil {
		return fail(fmt.Errorf("write sftp install id: %w", err))
	}
	if err := file.Sync(); err != nil {
		return fail(fmt.Errorf("sync sftp install id: %w", err))
	}
	if err := file.Close(); err != nil {
		return fail(fmt.Errorf("close sftp install id: %w", err))
	}
	// syncDirectory (ops.go) durability-fsyncs the directory entry so the
	// freshly minted id survives a crash.
	syncDirectory(filepath.Dir(idPath))
	return id, nil
}
