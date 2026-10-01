package backupsftp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/safefs"
)

const (
	partialSuffix = ".part"
	sidecarSuffix = ".sha256"
	// maxSidecarBytes bounds the sha256 sidecar read.
	maxSidecarBytes = 256
	// magicProbeBytes is how many leading bytes of a local archive Upload
	// reads to verify the Veil encrypted-archive header.
	magicProbeBytes = 64
	// fetchSlackBytes is how far past the remote-declared size a fetch may
	// read before the stream is cut — just enough to prove overshoot.
	fetchSlackBytes = 4096
)

// watchRemote closes the remote filesystem when ctx ends, so an in-flight
// SFTP request fails instead of wedging on a stalled server or surviving
// caller cancellation (#1202). The returned func detaches the watcher; it is
// a no-op for contexts that cannot be cancelled.
func watchRemote(ctx context.Context, fs RemoteFS) func() {
	done := ctx.Done()
	if done == nil {
		return func() {}
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-done:
			_ = fs.Close()
		case <-stop:
		}
	}()
	return func() { close(stop) }
}

// remoteOpErr reports ctx.Err() when cancellation is what broke the remote
// operation, so callers see context.Canceled rather than the transport's
// "connection closed" symptom.
func remoteOpErr(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

// ErrUnencryptedArchive rejects plaintext at the remote-publish boundary:
// the SFTP destination is an encrypted-only tier, the mirror image of the
// fetch side's .enc-only policy (#1188). Callers unwrap it with errors.Is to
// distinguish a policy refusal from a transport failure.
var ErrUnencryptedArchive = errors.New("unencrypted archives are not uploaded to the SFTP destination")

// UploadReceipt describes one verified remote upload.
type UploadReceipt struct {
	Archive string `json:"archive"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

// remotePath joins remote directory and basename with POSIX slash semantics.
func remotePath(dir, name string) string {
	return path.Join(dir, name)
}

// Upload copies localPath to <remoteDir>/<name> over the given remote FS:
// the bytes stream through a sha256 into a .part sibling that is renamed to
// the final name only after a remote size check, and a <name>.sha256
// sidecar is written so later downloads can verify content, not just size.
//
// Remote storage is encrypted-only: a name without the .enc suffix or a file
// missing the Veil encrypted-archive magic is refused with
// ErrUnencryptedArchive before a single byte leaves the host (#1188).
// probeLocalEncryptedArchive verifies the encrypted-archive magic on the
// local file so callers can refuse plaintext before the remote is ever
// dialed. Upload re-checks on its own no-follow handle (#1188).
func probeLocalEncryptedArchive(localPath string) error {
	local, err := safefs.OpenNoFollow(localPath)
	if err != nil {
		return fmt.Errorf("open local archive: %w", err)
	}
	defer local.Close()
	prefix := make([]byte, magicProbeBytes)
	n, _ := io.ReadFull(local, prefix)
	if !backup.IsEncryptedArchivePrefix(prefix[:n]) {
		return ErrUnencryptedArchive
	}
	return nil
}

func Upload(ctx context.Context, fs RemoteFS, config Config, localPath, name string) (UploadReceipt, error) {
	if err := ctx.Err(); err != nil {
		return UploadReceipt{}, err
	}
	defer watchRemote(ctx, fs)()
	if name == "" || path.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return UploadReceipt{}, fmt.Errorf("remote archive name %q must be a basename", name)
	}
	if !strings.HasSuffix(strings.ToLower(name), ".enc") {
		return UploadReceipt{}, fmt.Errorf("%w: %q is not an encrypted (.enc) archive", ErrUnencryptedArchive, name)
	}
	local, err := safefs.OpenNoFollow(localPath)
	if err != nil {
		return UploadReceipt{}, fmt.Errorf("open local archive: %w", err)
	}
	defer local.Close()
	info, err := local.Stat()
	if err != nil {
		return UploadReceipt{}, fmt.Errorf("stat local archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return UploadReceipt{}, fmt.Errorf("local archive %s is not a regular file", name)
	}
	// The name check alone is not enough: --output can hand over a
	// .enc-named file whose content was never encrypted, so the archive
	// magic itself is verified before any remote mutation (#1188).
	prefix := make([]byte, magicProbeBytes)
	n, _ := io.ReadFull(local, prefix)
	if !backup.IsEncryptedArchivePrefix(prefix[:n]) {
		return UploadReceipt{}, fmt.Errorf("%w: %q does not start with the Veil encrypted-archive header", ErrUnencryptedArchive, name)
	}
	if err := fs.MkdirAll(config.RemoteDir); err != nil {
		return UploadReceipt{}, fmt.Errorf("create remote directory: %w", err)
	}
	part := remotePath(config.RemoteDir, name+partialSuffix)
	// A leftover .part from an interrupted upload must not fail this run.
	_ = fs.Remove(part)
	dst, err := fs.Create(part)
	if err != nil {
		return UploadReceipt{}, fmt.Errorf("create remote archive: %w", err)
	}
	hash := sha256.New()
	// The probe bytes consumed by the header check re-join the stream so the
	// uploaded content and the digest cover the whole file.
	_, copyErr := io.Copy(dst, io.TeeReader(io.MultiReader(bytes.NewReader(prefix[:n]), local), hash))
	closeErr := dst.Close()
	if copyErr != nil {
		_ = fs.Remove(part)
		return UploadReceipt{}, remoteOpErr(ctx, fmt.Errorf("write remote archive: %w", copyErr))
	}
	if closeErr != nil {
		_ = fs.Remove(part)
		return UploadReceipt{}, fmt.Errorf("close remote archive: %w", closeErr)
	}
	if err := ctx.Err(); err != nil {
		_ = fs.Remove(part)
		return UploadReceipt{}, err
	}
	final := remotePath(config.RemoteDir, name)
	if err := fs.Rename(part, final); err != nil {
		_ = fs.Remove(part)
		return UploadReceipt{}, fmt.Errorf("publish remote archive: %w", err)
	}
	// Post-publish cleanup: a failure from here on must not leave a
	// verify-failed archive (or a half-written sidecar) on the remote —
	// the operator would otherwise see a "successful" archive listing whose
	// content was never verified.
	cleanup := func() {
		_ = fs.Remove(final)
		_ = fs.Remove(remotePath(config.RemoteDir, name+sidecarSuffix))
	}
	stat, err := fs.Stat(final)
	if err != nil {
		cleanup()
		return UploadReceipt{}, fmt.Errorf("verify remote archive: %w", err)
	}
	if stat.Size() != info.Size() {
		cleanup()
		return UploadReceipt{}, fmt.Errorf("verify remote archive: size %d != local %d", stat.Size(), info.Size())
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	sidecar := fmt.Sprintf("%s  %s\n", digest, name)
	sidecarName := remotePath(config.RemoteDir, name+sidecarSuffix)
	writer, err := fs.Create(sidecarName)
	if err != nil {
		cleanup()
		return UploadReceipt{}, fmt.Errorf("write remote checksum sidecar: %w", err)
	}
	if _, err := writer.Write([]byte(sidecar)); err != nil {
		_ = writer.Close()
		cleanup()
		return UploadReceipt{}, fmt.Errorf("write remote checksum sidecar: %w", err)
	}
	if err := writer.Close(); err != nil {
		cleanup()
		return UploadReceipt{}, fmt.Errorf("write remote checksum sidecar: %w", err)
	}
	return UploadReceipt{Archive: name, Size: info.Size(), SHA256: digest}, nil
}

// List returns the managed archives present in the remote directory in
// newest-first order, using the same managed-name filter the local listing
// applies. A missing remote directory lists as empty.
func List(ctx context.Context, fs RemoteFS, config Config) ([]backup.ArchiveEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	defer watchRemote(ctx, fs)()
	infos, err := fs.ReadDir(config.RemoteDir)
	if errors.Is(err, os.ErrNotExist) {
		return []backup.ArchiveEntry{}, nil
	}
	if err != nil {
		return nil, remoteOpErr(ctx, fmt.Errorf("list remote directory: %w", err))
	}
	entries := make([]backup.ArchiveEntry, 0, len(infos))
	for _, info := range infos {
		if info.IsDir() || !info.Mode().IsRegular() {
			continue
		}
		createdAt, ok := backup.ArchiveTimestamp(info.Name())
		if !ok {
			continue
		}
		entries = append(entries, backup.ArchiveEntry{
			Name:      info.Name(),
			Path:      remotePath(config.RemoteDir, info.Name()),
			Size:      info.Size(),
			CreatedAt: createdAt,
			Encrypted: strings.HasSuffix(info.Name(), ".enc"),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
			return entries[i].Name > entries[j].Name
		}
		return entries[i].CreatedAt.After(entries[j].CreatedAt)
	})
	return entries, nil
}

// Prune applies the shared daily/weekly/monthly bucket decision to the
// remote listing and removes remote archives (plus their .sha256 sidecars)
// the policy would not keep. This mirrors local PruneArchives semantics.
func Prune(ctx context.Context, fs RemoteFS, config Config, policy backup.RetentionPolicy) (backup.PruneResult, error) {
	if policy.Daily < 0 || policy.Weekly < 0 || policy.Monthly < 0 {
		return backup.PruneResult{}, errors.New("retention counts cannot be negative")
	}
	if err := ctx.Err(); err != nil {
		return backup.PruneResult{}, err
	}
	defer watchRemote(ctx, fs)()
	entries, err := List(ctx, fs, config)
	if err != nil {
		return backup.PruneResult{}, remoteOpErr(ctx, err)
	}
	keep := backup.RetentionKeepSet(entries, policy)
	result := backup.PruneResult{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if keep[entry.Name] {
			result.Kept = append(result.Kept, entry.Name)
			continue
		}
		if err := fs.Remove(entry.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			// Same contract as the local prune: partial progress travels with
			// the error so the operator can reconcile what is actually gone.
			return result, fmt.Errorf("remove remote archive %s: %w", entry.Name, err)
		}
		if err := fs.Remove(entry.Path + sidecarSuffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return result, fmt.Errorf("remove remote sidecar for %s: %w", entry.Name, err)
		}
		result.Deleted = append(result.Deleted, entry.Name)
	}
	return result, nil
}

// Fetch downloads one remote archive into localDir atomically: the bytes
// land at a hidden .part sibling first, the sha256 sidecar is verified when
// the remote carries one, size is checked against the remote stat, and the
// file is published by hardlink so a name that already exists locally can
// never be replaced.
func Fetch(ctx context.Context, fs RemoteFS, config Config, localDir, name string) (backup.ArchiveEntry, error) {
	if err := ctx.Err(); err != nil {
		return backup.ArchiveEntry{}, err
	}
	defer watchRemote(ctx, fs)()
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return backup.ArchiveEntry{}, fmt.Errorf("remote archive name %q must be a basename", name)
	}
	if _, ok := backup.ArchiveTimestamp(name); !ok {
		return backup.ArchiveEntry{}, fmt.Errorf("unrecognized remote archive name %q", name)
	}
	// Match the privileged fetch policy: only encrypted archives leave the
	// remote, so a plaintext blob parked on the SFTP side can never be pulled
	// into the managed restore set through this path.
	if !strings.HasSuffix(strings.ToLower(name), ".enc") {
		return backup.ArchiveEntry{}, fmt.Errorf("remote archive %q is not an encrypted archive", name)
	}
	final := filepath.Join(localDir, name)
	if _, err := os.Stat(final); err == nil {
		return backup.ArchiveEntry{}, fmt.Errorf("archive %s already exists locally", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return backup.ArchiveEntry{}, err
	}
	remoteInfo, err := fs.Stat(remotePath(config.RemoteDir, name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return backup.ArchiveEntry{}, os.ErrNotExist
		}
		return backup.ArchiveEntry{}, fmt.Errorf("stat remote archive: %w", err)
	}
	// Bound the download before any byte lands on the root-written backup
	// dir: reject a declared size the local policy could never restore, and
	// otherwise cut the stream at the remote-declared size plus slack, never
	// past the configured archive ceiling — a hostile server can lie about
	// sizes, but it cannot stream unbounded data (#1202).
	maxBytes, err := backup.ConfiguredMaxBackupBytes()
	if err != nil {
		return backup.ArchiveEntry{}, err
	}
	if remoteInfo.Size() > maxBytes {
		return backup.ArchiveEntry{}, fmt.Errorf("remote archive %s is %d bytes, over the %d-byte maximum", name, remoteInfo.Size(), maxBytes)
	}
	src, err := fs.Open(remotePath(config.RemoteDir, name))
	if err != nil {
		return backup.ArchiveEntry{}, fmt.Errorf("open remote archive: %w", err)
	}
	defer src.Close()
	if err := os.MkdirAll(localDir, 0o700); err != nil {
		return backup.ArchiveEntry{}, err
	}
	tmp, err := os.CreateTemp(localDir, ".veil-remote-fetch-*")
	if err != nil {
		return backup.ArchiveEntry{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	limit := remoteInfo.Size() + fetchSlackBytes
	if limit < 0 || limit > maxBytes {
		limit = maxBytes
	}
	hash := sha256.New()
	if _, err := io.Copy(tmp, io.TeeReader(io.LimitReader(src, limit), hash)); err != nil {
		_ = tmp.Close()
		return backup.ArchiveEntry{}, remoteOpErr(ctx, fmt.Errorf("download remote archive: %w", err))
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return backup.ArchiveEntry{}, err
	}
	if err := tmp.Close(); err != nil {
		return backup.ArchiveEntry{}, err
	}
	info, err := os.Stat(tmpPath)
	if err != nil {
		return backup.ArchiveEntry{}, err
	}
	if info.Size() != remoteInfo.Size() {
		return backup.ArchiveEntry{}, fmt.Errorf("downloaded size %d != remote %d", info.Size(), remoteInfo.Size())
	}
	verified, err := verifyRemoteSidecar(fs, config, name, hex.EncodeToString(hash.Sum(nil)))
	if err != nil {
		return backup.ArchiveEntry{}, remoteOpErr(ctx, err)
	}
	if !verified {
		// A pre-sidecar archive carries no digest to check: still prove the
		// downloaded bytes are a Veil encrypted archive instead of trusting
		// the .enc name before publishing into the managed restore set
		// (#1209).
		if err := probeLocalEncryptedArchive(tmpPath); err != nil {
			if errors.Is(err, ErrUnencryptedArchive) {
				err = fmt.Errorf("%w: %q does not start with the Veil encrypted-archive header", err, name)
			}
			return backup.ArchiveEntry{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return backup.ArchiveEntry{}, err
	}
	// Link fails with EEXIST instead of replacing an archive that appeared
	// meanwhile — same publish discipline as backup create.
	if err := os.Link(tmpPath, final); err != nil {
		return backup.ArchiveEntry{}, fmt.Errorf("publish downloaded archive: %w", err)
	}
	if err := os.Remove(tmpPath); err != nil {
		return backup.ArchiveEntry{}, err
	}
	syncDirectory(localDir)
	createdAt, _ := backup.ArchiveTimestamp(name)
	return backup.ArchiveEntry{
		Name:      name,
		Path:      final,
		Size:      info.Size(),
		CreatedAt: createdAt,
		Encrypted: strings.HasSuffix(name, ".enc"),
	}, nil
}

// verifyRemoteSidecar compares the downloaded digest with <name>.sha256 when
// the remote carries a sidecar, returning whether a sidecar verified the
// content. Missing sidecars are tolerated (archives uploaded before sidecars
// existed) but reported unverified so the caller can apply the format check
// instead; a mismatched one fails the fetch.
func verifyRemoteSidecar(fs RemoteFS, config Config, name, digest string) (bool, error) {
	body, err := fs.ReadFile(remotePath(config.RemoteDir, name+sidecarSuffix))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read remote checksum sidecar: %w", err)
	}
	if len(body) > maxSidecarBytes {
		return false, errors.New("remote checksum sidecar exceeds the size limit")
	}
	fields := strings.Fields(string(body))
	if len(fields) != 2 || fields[1] != name {
		return false, fmt.Errorf("remote checksum sidecar for %s is malformed", name)
	}
	expected, err := hex.DecodeString(fields[0])
	if err != nil || len(expected) != sha256.Size {
		return false, fmt.Errorf("remote checksum sidecar for %s carries an invalid digest", name)
	}
	if !equalHexDigest(fields[0], digest) {
		return false, fmt.Errorf("downloaded archive %s does not match its remote checksum", name)
	}
	return true, nil
}

func equalHexDigest(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

var syncDirectory = func(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}
