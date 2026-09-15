package privileged

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func TestPromotionRollbackReappliesRuntimeArtifactOwnership(t *testing.T) {
	oldEffectiveUID := effectiveUID
	oldLookupUser := lookupUser
	oldChownPath := chownPath
	oldChmodPath := chmodPath
	defer func() {
		effectiveUID = oldEffectiveUID
		lookupUser = oldLookupUser
		chownPath = oldChownPath
		chmodPath = oldChmodPath
	}()

	effectiveUID = func() int { return 0 }
	lookupUser = func(name string) (*user.User, error) {
		if name != "veil" {
			t.Fatalf("lookup user = %q, want veil", name)
		}
		return &user.User{Uid: "123", Gid: "456"}, nil
	}

	root := t.TempDir()
	protocol1Src := filepath.Join(root, "staging", "hysteria2", "edge.yaml")
	protocol1Dst := filepath.Join(root, "generated", "hysteria2", "edge.yaml")
	caddySrc := filepath.Join(root, "staging", "caddy", "config.json")
	caddyDst := filepath.Join(root, "generated", "caddy", "config.json")
	protocol2Src := filepath.Join(root, "staging", "hysteria2", "core.yaml")
	protocol2Dst := filepath.Join(root, "generated", "hysteria2", "core.yaml")
	writePromotionPair(t, protocol1Src, protocol1Dst, "new-edge", "old-edge")
	writePromotionPair(t, caddySrc, caddyDst, `{"apps":{}}`, `{"apps":{"old":true}}`)
	writePromotionPair(t, protocol2Src, protocol2Dst, "new-core", "old-core")

	var afterFail []ownershipCall
	failed := false
	chownPath = func(path string, uid, gid int) error {
		if failed {
			afterFail = append(afterFail, ownershipCall{path: path, uid: uid, gid: gid})
			return nil
		}
		if path == protocol2Dst {
			failed = true
			return errors.New("injected chown failure")
		}
		return nil
	}
	chmodPath = func(string, os.FileMode) error { return nil }

	executor := NewProductionExecutor(ProductionConfig{PromotionBackupRoot: filepath.Join(root, "backups")})
	_, err := executor.Promote(context.Background(), ResolvedPromotion{
		Artifacts: []ResolvedArtifact{
			{ID: "hysteria2/edge.yaml", Source: protocol1Src, Destination: protocol1Dst},
			{ID: "caddy/config.json", Source: caddySrc, Destination: caddyDst},
			{ID: "hysteria2/core.yaml", Source: protocol2Src, Destination: protocol2Dst},
		},
	})
	if err == nil {
		t.Fatal("expected injected second-artifact ownership error")
	}

	assertFileContent(t, protocol1Dst, "old-edge")
	assertFileContent(t, caddyDst, `{"apps":{"old":true}}`)
	assertFileContent(t, protocol2Dst, "old-core")

	if !hasChown(afterFail, protocol1Dst, 123, 456) {
		t.Fatalf("restored protocol artifact was not chowned veil:veil: %+v", afterFail)
	}
	if !hasChown(afterFail, caddyDst, 0, 456) {
		t.Fatalf("restored caddy artifact was not chowned root:veil: %+v", afterFail)
	}
}

func TestPromotionRecoveryReappliesRuntimeArtifactOwnership(t *testing.T) {
	oldEffectiveUID := effectiveUID
	oldLookupUser := lookupUser
	oldChownPath := chownPath
	oldChmodPath := chmodPath
	defer func() {
		effectiveUID = oldEffectiveUID
		lookupUser = oldLookupUser
		chownPath = oldChownPath
		chmodPath = oldChmodPath
	}()

	effectiveUID = func() int { return 0 }
	lookupUser = func(name string) (*user.User, error) {
		if name != "veil" {
			t.Fatalf("lookup user = %q, want veil", name)
		}
		return &user.User{Uid: "123", Gid: "456"}, nil
	}

	root := t.TempDir()
	backupRoot := filepath.Join(root, "backups")
	protocolDst := filepath.Join(root, "generated", "hysteria2", "edge.yaml")
	caddyDst := filepath.Join(root, "generated", "caddy", "config.json")
	protocolSafety := filepath.Join(backupRoot, "safety", "hysteria2", "edge.yaml")
	caddySafety := filepath.Join(backupRoot, "safety", "caddy", "config.json")
	oldProtocol, newProtocol := []byte("old-edge"), []byte("new-edge")
	oldCaddy, newCaddy := []byte(`{"apps":{"old":true}}`), []byte(`{"apps":{}}`)
	writeFileWithParents(t, protocolDst, newProtocol)
	writeFileWithParents(t, caddyDst, newCaddy)
	writeFileWithParents(t, protocolSafety, oldProtocol)
	writeFileWithParents(t, caddySafety, oldCaddy)

	journal := promotionTransactionJournal{
		Version: 1, TransactionID: "tx-1", Kind: "promotion", Phase: "artifact-2-published",
		Manifest: promotionManifest{
			Version: 1, TransactionID: "tx-1", BackupID: "safety", Kind: "promotion", Phase: "prepared",
			Records: []promotionManifestRecord{
				{
					ArtifactID: "hysteria2/edge.yaml", Destination: protocolDst, Operation: "write",
					HadPrevious: true, SafetyPath: protocolSafety, BackupPath: protocolSafety,
					OldDigest: promotionDigest(oldProtocol), NewDigest: promotionDigest(newProtocol),
					Phase: "published",
				},
				{
					ArtifactID: "caddy/config.json", Destination: caddyDst, Operation: "write",
					HadPrevious: true, SafetyPath: caddySafety, BackupPath: caddySafety,
					OldDigest: promotionDigest(oldCaddy), NewDigest: promotionDigest(newCaddy),
					Phase: "published",
				},
			},
		},
	}
	if err := writePromotionJournal(backupRoot, journal); err != nil {
		t.Fatalf("write crash journal: %v", err)
	}

	var chowns []ownershipCall
	chownPath = func(path string, uid, gid int) error {
		chowns = append(chowns, ownershipCall{path: path, uid: uid, gid: gid})
		return nil
	}
	chmodPath = func(string, os.FileMode) error { return nil }

	if _, err := promoteResolvedArtifacts(backupRoot, fixedPromotionNow, ResolvedPromotion{}); err != nil {
		t.Fatalf("recover interrupted promotion: %v", err)
	}
	assertFileContent(t, protocolDst, "old-edge")
	assertFileContent(t, caddyDst, `{"apps":{"old":true}}`)
	if !hasChown(chowns, protocolDst, 123, 456) {
		t.Fatalf("recovered protocol artifact was not chowned veil:veil: %+v", chowns)
	}
	if !hasChown(chowns, caddyDst, 0, 456) {
		t.Fatalf("recovered caddy artifact was not chowned root:veil: %+v", chowns)
	}
}

func writePromotionPair(t *testing.T, source, destination, newBody, oldBody string) {
	t.Helper()
	writeFileWithParents(t, source, []byte(newBody))
	writeFileWithParents(t, destination, []byte(oldBody))
}

func writeFileWithParents(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(body) != want {
		t.Fatalf("%s = %q, want %q", path, body, want)
	}
}

type ownershipCall struct {
	path     string
	uid, gid int
}

func hasChown(calls []ownershipCall, path string, uid, gid int) bool {
	for _, call := range calls {
		if call.path == path && call.uid == uid && call.gid == gid {
			return true
		}
	}
	return false
}
