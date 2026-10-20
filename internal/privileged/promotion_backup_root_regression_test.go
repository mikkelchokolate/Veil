package privileged

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression suite for #1274: the promotion backup root lives under the
// service-writable state tree. Its journal records (SafetyPath, ManifestPath)
// and restore manifests used to be trusted after a lexical pathWithin plus a
// leaf-only O_NOFOLLOW, so a symlinked intermediate directory turned recovery
// into an arbitrary root read (copied into a proxy-readable destination) and
// an arbitrary root delete, and a veil-owned replacement root let the service
// account author all of it.

const promotionRootTestSecret = "TOP-SECRET-OUTSIDE-BACKUP-ROOT"

type promotionRootFixture struct {
	root        string
	backupRoot  string
	outside     string
	destination string
	validate    func(string, string) bool
}

func newPromotionRootFixture(t *testing.T) promotionRootFixture {
	t.Helper()
	root := t.TempDir()
	f := promotionRootFixture{
		root:        root,
		backupRoot:  filepath.Join(root, "promotion-backups"),
		outside:     filepath.Join(root, "outside"),
		destination: filepath.Join(root, "generated", "caddy", "config.json"),
		validate:    allowPromotionDestinationsUnder(filepath.Join(root, "generated")),
	}
	if err := os.Mkdir(f.backupRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFileWithParents(t, f.destination, []byte("live"))
	writeFileWithParents(t, filepath.Join(f.outside, "secret"), []byte(promotionRootTestSecret))
	writeFileWithParents(t, filepath.Join(f.outside, "victim"), []byte("keep"))
	return f
}

// plantSymlinkedSubdir models what the service account can do to a backup
// root it controls (or races): an intermediate directory that is really a
// symlink to an arbitrary location.
func (f promotionRootFixture) plantSymlinkedSubdir(t *testing.T, name string) string {
	t.Helper()
	link := filepath.Join(f.backupRoot, name)
	if err := os.Symlink(f.outside, link); err != nil {
		t.Fatal(err)
	}
	return link
}

func (f promotionRootFixture) restoreJournal(safetyPath, oldDigest, manifestPath string) promotionTransactionJournal {
	return promotionTransactionJournal{
		Version: 1, TransactionID: "planted", Kind: "promotion", Phase: "artifact-1-published",
		ManifestPath: manifestPath,
		Manifest: promotionManifest{
			Version: 1, BackupID: "planted", Kind: "promotion", Phase: "prepared",
			Records: []promotionManifestRecord{{
				ArtifactID: "caddy/config.json", Destination: f.destination,
				BackupPath: safetyPath, SafetyPath: safetyPath,
				HadPrevious: true, OldDigest: oldDigest, Phase: "published",
			}},
		},
	}
}

func (f promotionRootFixture) writeRestoreManifest(t *testing.T, backupDir, backupID string, record promotionManifestRecord) {
	t.Helper()
	body, err := json.Marshal(promotionManifest{BackupID: backupID, Records: []promotionManifestRecord{record}})
	if err != nil {
		t.Fatal(err)
	}
	writeFileWithParents(t, filepath.Join(backupDir, "manifest.json"), body)
}

func assertDestinationNotExfiltrated(t *testing.T, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(body), promotionRootTestSecret) {
		t.Fatalf("file outside the backup root was copied into destination %s", path)
	}
	if string(body) != "live" {
		t.Fatalf("%s = %q, want the untouched live config", path, body)
	}
}

// Primitive A: a journal SafetyPath resolving through a symlinked directory
// inside the root must not be read — even with a matching digest.
func TestPromotionRecoveryRefusesSafetyPathThroughSymlinkedDirectory(t *testing.T) {
	f := newPromotionRootFixture(t)
	link := f.plantSymlinkedSubdir(t, "x")
	stagePromotionJournal(t, f.backupRoot, f.restoreJournal(
		filepath.Join(link, "secret"), promotionDigest([]byte(promotionRootTestSecret)), ""))

	if err := recoverPromotionTransactionWithPolicy(f.backupRoot, f.validate, 0); err == nil {
		t.Fatal("recovery read a safety copy through a symlinked backup directory")
	}
	assertDestinationNotExfiltrated(t, f.destination)
}

// Primitive B: a journal ManifestPath resolving through a symlinked directory
// must not be unlinked.
func TestPromotionRecoveryRefusesManifestRemovalThroughSymlinkedDirectory(t *testing.T) {
	f := newPromotionRootFixture(t)
	link := f.plantSymlinkedSubdir(t, "x")
	stagePromotionJournal(t, f.backupRoot, promotionTransactionJournal{
		Version: 1, TransactionID: "planted", Kind: "promotion", Phase: "prepared",
		ManifestPath: filepath.Join(link, "victim"),
		Manifest:     promotionManifest{Version: 1, BackupID: "planted", Kind: "promotion", Phase: "prepared"},
	})

	if err := recoverPromotionTransactionWithPolicy(f.backupRoot, f.validate, 0); err == nil {
		t.Fatal("recovery removed a manifest path through a symlinked backup directory")
	}
	assertFileContent(t, filepath.Join(f.outside, "victim"), "keep")
}

// A HadPrevious record without a digest is unauthenticated: recovery must not
// treat the empty digest as "skip the check".
func TestPromotionRecoveryRejectsSafetyRecordWithoutDigest(t *testing.T) {
	f := newPromotionRootFixture(t)
	safety := filepath.Join(f.backupRoot, "planted", "caddy", "config.json")
	writeFileWithParents(t, safety, []byte("planted"))
	stagePromotionJournal(t, f.backupRoot, f.restoreJournal(safety, "", ""))

	if err := recoverPromotionTransactionWithPolicy(f.backupRoot, f.validate, 0); err == nil {
		t.Fatal("recovery restored a safety record that carries no digest")
	}
	assertFileContent(t, f.destination, "live")
}

// The root contract: recovery trusts the journal only inside a helper-owned
// 0700 directory. The control leg proves the identical journal is replayed
// once the root satisfies the contract, so the rejection is the contract and
// not some other validation.
func TestPromotionRecoveryRequiresHelperOwned0700BackupRoot(t *testing.T) {
	stage := func(t *testing.T) promotionRootFixture {
		f := newPromotionRootFixture(t)
		safety := filepath.Join(f.backupRoot, "planted", "caddy", "config.json")
		writeFileWithParents(t, safety, []byte("previous"))
		stagePromotionJournal(t, f.backupRoot, f.restoreJournal(safety, promotionDigest([]byte("previous")), ""))
		return f
	}

	t.Run("group-accessible root", func(t *testing.T) {
		f := stage(t)
		if err := os.Chmod(f.backupRoot, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := recoverPromotionTransactionWithPolicy(f.backupRoot, f.validate, 0); err == nil {
			t.Fatal("recovery trusted a journal in a non-0700 backup root")
		}
		assertFileContent(t, f.destination, "live")
	})

	t.Run("root owned by another uid", func(t *testing.T) {
		f := stage(t)
		previous := promotionRootOwnerUID
		t.Cleanup(func() { promotionRootOwnerUID = previous })
		promotionRootOwnerUID = func() int { return os.Geteuid() + 1 }
		if err := recoverPromotionTransactionWithPolicy(f.backupRoot, f.validate, 0); err == nil {
			t.Fatal("recovery trusted a journal in a backup root the helper does not own")
		}
		assertFileContent(t, f.destination, "live")
	})

	t.Run("control: helper-owned 0700 root", func(t *testing.T) {
		f := stage(t)
		if err := recoverPromotionTransactionWithPolicy(f.backupRoot, f.validate, 0); err != nil {
			t.Fatalf("recovery in a helper-owned 0700 root: %v", err)
		}
		assertFileContent(t, f.destination, "previous")
	})
}

// The fresh-promotion path pins the same contract before taking the lock.
func TestPromotionRefusesNon0700BackupRoot(t *testing.T) {
	f := newPromotionRootFixture(t)
	if err := os.Chmod(f.backupRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(f.root, "staging", "caddy", "config.json")
	writeFileWithParents(t, source, []byte("next"))
	executor := NewProductionExecutor(ProductionConfig{PromotionBackupRoot: f.backupRoot})
	_, err := executor.Promote(context.Background(), ResolvedPromotion{
		Artifacts:           []ResolvedArtifact{{ID: "caddy/config.json", Source: source, Destination: f.destination}},
		ValidateDestination: f.validate,
	})
	if err == nil || !strings.Contains(err.Error(), "helper-owned 0700") {
		t.Fatalf("promote error = %v, want the backup-root contract rejection", err)
	}
	assertFileContent(t, f.destination, "live")
}

const promotionRootTestBackupID = "20260605T120000.000000000Z"

// Primitive C: a restore manifest record whose safety copy resolves through a
// symlinked directory must not be staged into the rollback transaction.
func TestRestorePromotedArtifactsRefusesSafetyPathThroughSymlinkedDirectory(t *testing.T) {
	f := newPromotionRootFixture(t)
	link := f.plantSymlinkedSubdir(t, "x")
	backupDir := filepath.Join(f.backupRoot, promotionRootTestBackupID)
	f.writeRestoreManifest(t, backupDir, promotionRootTestBackupID, promotionManifestRecord{
		ArtifactID: "caddy/config.json", Destination: f.destination,
		SafetyPath: filepath.Join(link, "secret"), HadPrevious: true,
		OldDigest: promotionDigest([]byte(promotionRootTestSecret)),
	})

	if _, err := restorePromotedArtifacts(f.backupRoot, promotionRootTestBackupID, f.validate, 0); err == nil {
		t.Fatal("restore read a safety copy through a symlinked backup directory")
	}
	assertDestinationNotExfiltrated(t, f.destination)
}

// The backup-ID directory itself may be a planted symlink: the manifest and
// its members must not be read through it.
func TestRestorePromotedArtifactsRefusesSymlinkedBackupDirectory(t *testing.T) {
	f := newPromotionRootFixture(t)
	f.writeRestoreManifest(t, f.outside, promotionRootTestBackupID, promotionManifestRecord{
		ArtifactID: "caddy/config.json", Destination: f.destination,
		SafetyPath:  filepath.Join(f.backupRoot, promotionRootTestBackupID, "secret"),
		HadPrevious: true, OldDigest: promotionDigest([]byte(promotionRootTestSecret)),
	})
	f.plantSymlinkedSubdir(t, promotionRootTestBackupID)

	_, err := restorePromotedArtifacts(f.backupRoot, promotionRootTestBackupID, f.validate, 0)
	if err == nil {
		t.Fatal("restore followed a symlinked backup directory")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restore error = %v, want a symlink refusal rather than a missing manifest", err)
	}
	assertDestinationNotExfiltrated(t, f.destination)
}

func TestRestorePromotedArtifactsRejectsSafetyRecordWithoutDigest(t *testing.T) {
	f := newPromotionRootFixture(t)
	backupDir := filepath.Join(f.backupRoot, promotionRootTestBackupID)
	safety := filepath.Join(backupDir, "caddy", "config.json")
	writeFileWithParents(t, safety, []byte("planted"))
	f.writeRestoreManifest(t, backupDir, promotionRootTestBackupID, promotionManifestRecord{
		ArtifactID: "caddy/config.json", Destination: f.destination, SafetyPath: safety, HadPrevious: true,
	})

	if _, err := restorePromotedArtifacts(f.backupRoot, promotionRootTestBackupID, f.validate, 0); err == nil {
		t.Fatal("restore accepted a safety record that carries no digest")
	}
	assertFileContent(t, f.destination, "live")
}

// Control for the restore path: a well-formed nested safety copy inside the
// pinned root is still restored, via the descriptor-read body.
func TestRestorePromotedArtifactsRestoresNestedSafetyCopy(t *testing.T) {
	f := newPromotionRootFixture(t)
	backupDir := filepath.Join(f.backupRoot, promotionRootTestBackupID)
	safety := filepath.Join(backupDir, "caddy", "config.json")
	writeFileWithParents(t, safety, []byte("previous"))
	f.writeRestoreManifest(t, backupDir, promotionRootTestBackupID, promotionManifestRecord{
		ArtifactID: "caddy/config.json", Destination: f.destination, SafetyPath: safety,
		HadPrevious: true, OldDigest: promotionDigest([]byte("previous")),
	})

	result, err := restorePromotedArtifacts(f.backupRoot, promotionRootTestBackupID, f.validate, 0)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(result.WrittenArtifacts) != 1 || result.WrittenArtifacts[0] != "caddy/config.json" {
		t.Fatalf("unexpected restore result: %+v", result)
	}
	assertFileContent(t, f.destination, "previous")
}
