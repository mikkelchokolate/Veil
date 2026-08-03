package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestUpdateRecoveryAcceptsOnlyExactHelperCommittedEvidence(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("positive helper evidence requires a root-controlled directory")
	}
	root := t.TempDir()
	binaryPath := filepath.Join(root, "veil")
	binary := []byte("verified-panel-binary")
	if err := os.WriteFile(binaryPath, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	inode := fmt.Sprintf("%d:%d:%d:%d", stat.Dev, stat.Ino, stat.Ctim.Sec, stat.Ctim.Nsec)
	digestBytes := sha256.Sum256(binary)
	digest := hex.EncodeToString(digestBytes[:])
	manifestPath := filepath.Join(root, ".veil-update-evidence.json")
	evidence, _ := json.Marshal(updateHelperEvidence{
		Version: 1, TransactionID: "update-operation", ExpectedBinaryDigest: digest,
		OldBinaryDigest: stringsOfLength("b", 64), InstalledPathInode: inode,
		TargetVersion: "v2.0.0", ActivationManifest: manifestPath, CommitPhase: "committed",
	})
	if err := os.WriteFile(manifestPath, evidence, 0o600); err != nil {
		t.Fatal(err)
	}
	state, details, err := readUpdateHelperEvidence(PublicationDetails{
		UpdateTransactionID: "update-operation", TargetVersion: "v2.0.0", ActivationManifest: manifestPath,
	})
	if err != nil || state != "committed" || details.ExpectedBinaryDigest != digest || details.InstalledInode != inode {
		t.Fatalf("state=%s details=%+v err=%v", state, details, err)
	}
	if err := os.WriteFile(binaryPath, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if state, _, err := readUpdateHelperEvidence(PublicationDetails{UpdateTransactionID: "update-operation", TargetVersion: "v2.0.0", ActivationManifest: manifestPath}); state != "invalid" || err == nil {
		t.Fatalf("tampered evidence state=%s err=%v", state, err)
	}
}

func stringsOfLength(value string, length int) string {
	result := ""
	for len(result) < length {
		result += value
	}
	return result[:length]
}
