package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
	"github.com/mikkelchokolate/Veil/internal/releaseverify"
)

const (
	panelSourceBuildSHA  = "ae5124ee9d0c651f86c9c3bc99c021056fc0b699"
	panelSourceTargetSHA = "6daa8560b6aab39b111782ca31294b35744a1f62"
)

func sourceBuildPanelUpdateStager(t *testing.T) (panelUpdateStager, *int) {
	t.Helper()
	archive := []byte("release-archive")
	checksums := []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), updateflow.AssetName()))
	files := map[string][]byte{
		"archive": archive, "checksums.txt": checksums, "checksums.txt.bundle": []byte("signed-evidence"),
		"veil.provenance.json": []byte("signed-provenance"), "veil.provenance.json.bundle": []byte("signed-evidence"),
	}
	downloads := 0
	return panelUpdateStager{
		root: t.TempDir(), version: "main-ae5124ee9d0c (" + panelSourceBuildSHA + ")", assetName: updateflow.AssetName(),
		latest: func(context.Context) (*updateflow.Release, error) {
			assets := []updateflow.Asset{{Name: updateflow.AssetName(), BrowserDownloadURL: "https://example.invalid/archive"}}
			for _, name := range []string{"checksums.txt", "checksums.txt.bundle", "veil.provenance.json", "veil.provenance.json.bundle"} {
				assets = append(assets, updateflow.Asset{Name: name, BrowserDownloadURL: "https://example.invalid/" + name})
			}
			return &updateflow.Release{TagName: "v0.7.3", Assets: assets}, nil
		},
		download: func(_ context.Context, url string) ([]byte, error) {
			downloads++
			body, ok := files[strings.TrimPrefix(url, "https://example.invalid/")]
			if !ok {
				return nil, fmt.Errorf("unexpected asset URL %q", url)
			}
			return body, nil
		},
		resolveCommit: func(context.Context, string) (string, error) { return panelSourceTargetSHA, nil },
		verify: func(evidence releaseverify.Evidence) error {
			if evidence.SourceCommit != panelSourceTargetSHA {
				t.Fatalf("release verification source=%q, want proof-pinned %s", evidence.SourceCommit, panelSourceTargetSHA)
			}
			return nil
		},
	}, &downloads
}

func TestPanelUpdateStagerAcceptsProvenSourceBuildWithoutRepinning(t *testing.T) {
	for _, current := range []string{"main-" + panelSourceBuildSHA, "main-ae5124ee9d0c (" + panelSourceBuildSHA + ")"} {
		t.Run(current, func(t *testing.T) {
			stager, downloads := sourceBuildPanelUpdateStager(t)
			stager.version = current
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			proofs, verified := 0, 0
			stager.sourceUpdateCommit = func(gotCtx context.Context, running, tag string) (string, error) {
				proofs++
				if gotCtx != ctx || running != current || tag != "v0.7.3" {
					t.Fatalf("proof input context=%v running=%q tag=%q", gotCtx == ctx, running, tag)
				}
				return panelSourceTargetSHA, nil
			}
			stager.resolveCommit = func(context.Context, string) (string, error) {
				t.Fatal("source proof must not be repinned by a second tag lookup")
				return "", nil
			}
			verify := stager.verify
			stager.verify = func(evidence releaseverify.Evidence) error { verified++; return verify(evidence) }
			version, err := stager.Stage(ctx, false)
			if err != nil || version != "v0.7.3" || proofs != 1 || verified != 1 || *downloads == 0 {
				t.Fatalf("version=%q error=%v proofs=%d verified=%d downloads=%d", version, err, proofs, verified, *downloads)
			}
			if _, err := os.Stat(filepath.Join(stager.root, "update-manifest.json")); err != nil {
				t.Fatalf("successful source update was not staged: %v", err)
			}
		})
	}
}

func TestPanelUpdateStagerRefusesMissingOrInvalidSourceProofBeforeDownloads(t *testing.T) {
	for _, tc := range []struct {
		name, commit string
		err          error
		missing      bool
	}{
		{name: "unconfigured", missing: true},
		{name: "unproven ancestry", err: errors.New("source is not an ancestor of the release")},
		{name: "lookup failure", err: errors.New("GitHub unavailable")},
		{name: "empty commit"},
		{name: "short commit", commit: panelSourceTargetSHA[:12]},
		{name: "nonhex commit", commit: strings.Repeat("g", 40)},
		{name: "long commit", commit: panelSourceTargetSHA + "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stager, downloads := sourceBuildPanelUpdateStager(t)
			if !tc.missing {
				stager.sourceUpdateCommit = func(context.Context, string, string) (string, error) { return tc.commit, tc.err }
			}
			stager.resolveCommit = func(context.Context, string) (string, error) {
				t.Fatal("refused proof must not resolve the tag again")
				return "", nil
			}
			_, err := stager.Stage(context.Background(), false)
			var refused *updateRefusedError
			if !errors.As(err, &refused) || *downloads != 0 {
				t.Fatalf("error=%v downloads=%d; want refusal before download", err, *downloads)
			}
			entries, readErr := os.ReadDir(stager.root)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("refused proof wrote staging files: entries=%v error=%v", entries, readErr)
			}
		})
	}
}

func TestPanelUpdateStagerSourceProofCannotBypassUnknownBuildOrSemverGuards(t *testing.T) {
	for _, current := range []string{"dev", "", "main-abcdef1", "main-ae5124ee9d0c", "main-" + strings.Repeat("g", 40), "v0.7.3", "v0.8.0", "v0.8.0-rc.1"} {
		t.Run(current, func(t *testing.T) {
			stager, downloads := sourceBuildPanelUpdateStager(t)
			stager.version = current
			stager.sourceUpdateCommit = func(context.Context, string, string) (string, error) {
				t.Fatal("unknown/non-newer release version must not use source proof")
				return panelSourceTargetSHA, nil
			}
			_, err := stager.Stage(context.Background(), false)
			var refused *updateRefusedError
			if !errors.As(err, &refused) || *downloads != 0 {
				t.Fatalf("running=%q error=%v downloads=%d", current, err, *downloads)
			}
		})
	}
}

func TestPanelUpdateStagerForcedSourceUpdateStillVerifiesSignedRelease(t *testing.T) {
	for _, invalidSignature := range []bool{false, true} {
		t.Run(fmt.Sprintf("invalidSignature=%t", invalidSignature), func(t *testing.T) {
			stager, downloads := sourceBuildPanelUpdateStager(t)
			stager.sourceUpdateCommit = func(context.Context, string, string) (string, error) {
				t.Fatal("explicit force must not require source ancestry proof")
				return "", nil
			}
			resolved, verified := 0, 0
			stager.resolveCommit = func(context.Context, string) (string, error) { resolved++; return panelSourceTargetSHA, nil }
			verify := stager.verify
			stager.verify = func(evidence releaseverify.Evidence) error {
				verified++
				if err := verify(evidence); err != nil {
					return err
				}
				if invalidSignature {
					return errors.New("invalid release signature")
				}
				return nil
			}
			version, err := stager.Stage(context.Background(), true)
			if (err != nil) != invalidSignature || resolved != 1 || verified != 1 || *downloads == 0 {
				t.Fatalf("version=%q error=%v resolved=%d verified=%d downloads=%d", version, err, resolved, verified, *downloads)
			}
			if invalidSignature {
				entries, readErr := os.ReadDir(stager.root)
				if readErr != nil || len(entries) != 0 {
					t.Fatalf("invalid signed release wrote staging files: entries=%v error=%v", entries, readErr)
				}
			}
		})
	}
}
