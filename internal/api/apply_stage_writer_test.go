package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
)

func TestWriteApplyStageWritesPlanSnapshotAndRenderedConfigs(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "generated", "caddy", "Caddyfile")
	written, validations, renderedPaths, err := WriteApplyStage(ApplyStageInput{
		ApplyRoot: root,
		Plan:      ApplyPlanResponse{Valid: true, Configs: []string{"caddy"}},
		Snapshot:  managementSnapshot{Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}},
		Rendered:  map[string]string{configPath: "caddy config"},
		Validate: func(paths []string) []ConfigValidationResult {
			if len(paths) != 1 || paths[0] != configPath {
				t.Fatalf("validation paths = %+v", paths)
			}
			return []ConfigValidationResult{{Name: "caddy", Valid: true}}
		},
	})
	if err != nil {
		t.Fatalf("WriteApplyStage: %v", err)
	}
	for _, path := range []string{
		filepath.Join(root, "generated", "veil", "apply-plan.json"),
		filepath.Join(root, "generated", "veil", "management-state.json"),
		configPath,
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected written file %s: %v", path, err)
		}
	}
	if len(written) != 3 || len(renderedPaths) != 1 || len(validations) != 1 {
		t.Fatalf("unexpected result: written=%+v rendered=%+v validations=%+v", written, renderedPaths, validations)
	}
}

func TestWriteApplyStageRendersHysteria2GeoMatchersAfterDatDownload(t *testing.T) {
	root := t.TempDir()
	hyPath := filepath.Join(root, "generated", "hysteria2", "server.yaml")
	geoBody := []byte("fake geosite dat")
	digest := sha256.Sum256(geoBody)
	oldDownloader := routeDatDownloader
	oldVerifier := routeDatSignatureVerifier
	routeDatSignatureVerifier = func(context.Context, generatedconfig.RoutingSourceFile, []byte, []byte) error { return nil }
	routeDatDownloader = func(_ context.Context, url string) ([]byte, error) {
		if strings.HasSuffix(url, "/geosite.dat") {
			return geoBody, nil
		}
		if strings.HasSuffix(url, "/geosite.dat.sha256sum") {
			return []byte(hex.EncodeToString(digest[:]) + " geosite.dat\n"), nil
		}
		t.Fatalf("unexpected URL %s", url)
		return nil, nil
	}
	t.Cleanup(func() {
		routeDatDownloader = oldDownloader
		routeDatSignatureVerifier = oldVerifier
	})

	var sawDat bool
	_, _, _, err := WriteApplyStage(ApplyStageInput{
		ApplyRoot: root,
		Plan:      ApplyPlanResponse{Valid: true, Configs: []string{"hysteria2"}},
		Snapshot:  managementSnapshot{Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}},
		RoutingSource: RoutingSource{Files: []RoutingSourceFile{{
			Name: "geosite.dat", URL: "https://example.test/geosite.dat",
			SHA256URL: "https://example.test/geosite.dat.sha256sum", PinnedSHA256: hex.EncodeToString(digest[:]),
		}}},
		Render: func() (map[string]string, error) {
			if _, err := os.Stat(filepath.Join(root, "generated", "rules", "geosite.dat")); err != nil {
				t.Fatalf("render ran before geosite.dat was on disk: %v", err)
			}
			sawDat = true
			return map[string]string{hyPath: "acl:\n  - direct(geosite:cn)\n  - warp(all)\n"}, nil
		},
		Validate: func([]string) []ConfigValidationResult { return nil },
	})
	if err != nil {
		t.Fatalf("WriteApplyStage: %v", err)
	}
	if !sawDat {
		t.Fatal("render callback was not invoked after routing dat download")
	}
	body, err := os.ReadFile(hyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "direct(geosite:cn)") {
		t.Fatalf("hysteria2 stage missing geo matcher:\n%s", body)
	}
}
