package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/applyflow"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
)

// The staging tree is writable by the veil uid. The promote request binds
// the helper to the bytes this process rendered and validated, so those
// digests must come from memory, never from re-reading staged files (#1269).

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func TestWriteApplyStageDigestsRenderedAndRoutingBytesFromMemory(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "generated", "caddy", "config.json")
	geoPath := filepath.Join(root, "generated", "rules", "geosite.dat")
	geoBody := []byte("verified geosite dat")
	geoDigest := sha256Hex(geoBody)
	oldDownloader := routeDatDownloader
	oldVerifier := routeDatSignatureVerifier
	routeDatSignatureVerifier = func(context.Context, generatedconfig.RoutingSourceFile, []byte, []byte) error { return nil }
	routeDatDownloader = func(_ context.Context, url string) ([]byte, error) {
		switch {
		case strings.HasSuffix(url, "/geosite.dat"):
			return geoBody, nil
		case strings.HasSuffix(url, "/geosite.dat.sha256sum"):
			return []byte(geoDigest + " geosite.dat\n"), nil
		}
		t.Fatalf("unexpected URL %s", url)
		return nil, nil
	}
	t.Cleanup(func() {
		routeDatDownloader = oldDownloader
		routeDatSignatureVerifier = oldVerifier
	})

	_, _, _, digests, err := writeApplyStage(ApplyStageInput{
		ApplyRoot: root,
		Plan:      ApplyPlanResponse{Valid: true, Configs: []string{"caddy"}},
		Snapshot:  managementSnapshot{Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}},
		RoutingSource: RoutingSource{Files: []RoutingSourceFile{{
			Name: "geosite.dat", URL: "https://example.test/geosite.dat",
			SHA256URL: "https://example.test/geosite.dat.sha256sum", PinnedSHA256: geoDigest,
		}}},
		Rendered: map[string]string{configPath: "rendered caddy config"},
		// A veil-uid writer rewrites both staged files before validation
		// even runs; the digests must still name what this process wrote.
		Validate: func([]string) []ConfigValidationResult {
			for _, path := range []string{configPath, geoPath} {
				if err := os.WriteFile(path, []byte("attacker bytes"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("writeApplyStage: %v", err)
	}
	if got, want := digests[configPath], sha256Hex([]byte("rendered caddy config")); got != want {
		t.Fatalf("rendered config digest=%q, want in-memory digest %q", got, want)
	}
	if got := digests[geoPath]; got != geoDigest {
		t.Fatalf("routing dat digest=%q, want verified digest %q", got, geoDigest)
	}
}

func TestPromoteStagedConfigsBindsHelperToStageTimeBytes(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	state.applyRoot = t.TempDir()
	state.liveRoot = filepath.Join(state.applyRoot, "live")
	state.inbounds = []Inbound{{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 8443, Enabled: true,
		Password: "secret"}}
	client := &recordingPrivilegedClient{statusActiveState: "inactive", statusLoadState: "not-found"}
	state.privileged = client
	ctx := NewManagementApplyContext(state)

	_, _, rendered, err := ctx.writeApplyStageLocked(ApplyPlanResponse{Valid: true})
	if err != nil {
		t.Fatalf("write apply stage: %v", err)
	}
	staged := ""
	for _, path := range rendered {
		if strings.HasSuffix(filepath.ToSlash(path), "mieru/server_config.json") {
			staged = path
		}
	}
	if staged == "" {
		t.Fatalf("mieru config was not staged: %+v", rendered)
	}
	stageTime, err := os.ReadFile(staged)
	if err != nil {
		t.Fatal(err)
	}
	// Rewritten after validation, before the helper reads it.
	if err := os.WriteFile(staged, []byte(`{"attacker":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ctx.promoteStagedConfigs(rendered); err != nil {
		t.Fatalf("promote staged configs: %v", err)
	}
	if len(client.promotions) != 1 {
		t.Fatalf("promotions=%+v", client.promotions)
	}
	request := client.promotions[0]
	if len(request.ArtifactDigests) != len(request.ArtifactIDs) {
		t.Fatalf("every promoted artifact needs a digest: ids=%v digests=%v", request.ArtifactIDs, request.ArtifactDigests)
	}
	if got, want := request.ArtifactDigests["mieru/server_config.json"], sha256Hex(stageTime); got != want {
		t.Fatalf("promote digest=%q, want stage-time digest %q (not the rewritten file)", got, want)
	}
}

func TestPromoteStagedConfigsRefusesArtifactWithoutStageTimeDigest(t *testing.T) {
	root := t.TempDir()
	staged := filepath.Join(root, "generated", "caddy", "config.json")
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("config"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &recordingPrivilegedClient{statusActiveState: "inactive"}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: root, Privileged: client})
	_, _, _, err := NewManagementApplyContext(state).promoteStagedConfigs([]string{staged})
	var preMutation *applyflow.PreMutationError
	if !errors.As(err, &preMutation) || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("promote without stage-time digest err=%v, want pre-mutation refusal", err)
	}
	if len(client.promotions) != 0 {
		t.Fatalf("helper was asked to promote unattested bytes: %+v", client.promotions)
	}
}

// attestStagedConfigsForTest records the stage-time digests a real stage
// write would have produced, for tests that stage files by hand and then
// drive promoteStagedConfigs directly.
func attestStagedConfigsForTest(t *testing.T, state *managementState, paths ...string) {
	t.Helper()
	digests := make(map[string]string, len(paths))
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("attest staged config %s: %v", path, err)
		}
		digests[filepath.Clean(path)] = sha256Hex(body)
	}
	state.stagedArtifactDigests = digests
}
