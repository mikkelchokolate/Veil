package privileged

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

// Protocol runtime units run as User=veil-proxy while the helper promotes
// generated configs as root with restrictive modes. Promotion must publish
// them root:veil-proxy with a group-readable file (0640) and traversable
// directories (0750), or every protocol service fails to read its config and
// the synchronous apply job never converges.
func TestPromotionPublishesProtocolArtifactReadableByVeilProxy(t *testing.T) {
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
		switch name {
		case "veil":
			return &user.User{Uid: "123", Gid: "456"}, nil
		case "veil-proxy":
			return &user.User{Uid: "124", Gid: "457"}, nil
		default:
			t.Fatalf("lookup user = %q, want veil or veil-proxy", name)
			return nil, nil
		}
	}
	var chowns []ownershipCall
	chownPath = func(path string, uid, gid int) error {
		chowns = append(chowns, ownershipCall{path: path, uid: uid, gid: gid})
		return nil
	}
	type chmodCall struct {
		path string
		mode os.FileMode
	}
	var chmods []chmodCall
	chmodPath = func(path string, mode os.FileMode) error {
		chmods = append(chmods, chmodCall{path: path, mode: mode})
		return nil
	}
	hasChmod := func(path string, mode os.FileMode) bool {
		for _, call := range chmods {
			if call.path == path && call.mode == mode {
				return true
			}
		}
		return false
	}

	root := t.TempDir()
	source := filepath.Join(root, "staging", "hysteria2", "edge.yaml")
	destination := filepath.Join(root, "generated", "hysteria2", "edge.yaml")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("listen: :443"), 0o600); err != nil {
		t.Fatal(err)
	}

	executor := NewProductionExecutor(ProductionConfig{PromotionBackupRoot: filepath.Join(root, "backups")})
	result, err := executor.Promote(context.Background(), ResolvedPromotion{
		Artifacts: []ResolvedArtifact{{
			ID:          "hysteria2/edge.yaml",
			Source:      source,
			Destination: destination,
		}},
	})
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if len(result.WrittenArtifacts) != 1 {
		t.Fatalf("unexpected promote result: %+v", result)
	}

	protocolDir := filepath.Dir(destination)
	generatedRoot := filepath.Dir(protocolDir)
	for _, path := range []string{destination, protocolDir, generatedRoot} {
		if !hasChown(chowns, path, 0, 457) {
			t.Fatalf("%s was not chowned root:veil-proxy: %+v", path, chowns)
		}
	}
	if !hasChmod(destination, 0o640) {
		t.Fatalf("protocol config was not chmodded 0640: %+v", chmods)
	}
	if !hasChmod(protocolDir, 0o750) {
		t.Fatalf("protocol directory was not chmodded 0750: %+v", chmods)
	}
	if !hasChmod(generatedRoot, 0o750) {
		t.Fatalf("generated root was not chmodded 0750: %+v", chmods)
	}
}

// Without a dedicated veil-proxy account the artifact must still become
// readable through the veil group rather than failing or staying root-only.
func TestRuntimeArtifactGIDFallsBackToVeilGroup(t *testing.T) {
	oldLookupUser := lookupUser
	defer func() { lookupUser = oldLookupUser }()
	lookupUser = func(name string) (*user.User, error) {
		if name == "veil" {
			return &user.User{Uid: "123", Gid: "456"}, nil
		}
		return nil, os.ErrNotExist
	}
	gid, err := runtimeArtifactGID()
	if err != nil {
		t.Fatalf("runtimeArtifactGID: %v", err)
	}
	if gid != 456 {
		t.Fatalf("gid = %d, want veil group 456", gid)
	}
}
