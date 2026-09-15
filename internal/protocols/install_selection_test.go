package protocols

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
)

func TestInstallRuntimesForFiltersCatalogBeforeInstall(t *testing.T) {
	ctx := context.Background()
	binDir := t.TempDir()

	r := NewRegistryRaw()
	r.Register(&mockRuntimeProvider{
		mockPlugin: &mockPlugin{protocol: "alpha", displayName: "Alpha"},
		runtime: runtimeinstall.Runtime{
			Name:    "alpha",
			Binary:  "alpha",
			Method:  runtimeinstall.MethodRawBinary,
			Repo:    "alpha/repo",
			Version: "v1", Integrity: "pinned-sha256", PinnedSHA256: "a8076d3d28d21e02012b20eaf7dbf75409a6277134439025f282e368e3305abf",
			VersionArgs: []string{"--version"}, VersionCommand: "alpha --version", VersionPattern: `v1`,
			AssetMatch: func(name string) bool { return name == "alpha" },
		},
	})
	r.Register(&mockRuntimeProvider{
		mockPlugin: &mockPlugin{protocol: "beta", displayName: "Beta"},
		runtime: runtimeinstall.Runtime{
			Name:    "beta",
			Binary:  "beta",
			Method:  runtimeinstall.MethodRawBinary,
			Repo:    "beta/repo",
			Version: "v1", Integrity: "pinned-sha256", PinnedSHA256: "a8076d3d28d21e02012b20eaf7dbf75409a6277134439025f282e368e3305abf",
			VersionArgs: []string{"--version"}, VersionCommand: "beta --version", VersionPattern: `v1`,
			AssetMatch: func(name string) bool { return name == "beta" },
		},
	})

	var fetched []string
	opts := runtimeinstall.Options{
		BinDir:     binDir,
		Arch:       "amd64",
		RunVersion: func(context.Context, string, []string) (string, error) { return "v1", nil },
		FetchRelease: func(ctx context.Context, repo string) (*runtimeinstall.Release, error) {
			fetched = append(fetched, repo)
			if repo != "alpha/repo" {
				return nil, fmt.Errorf("unexpected repo %s", repo)
			}
			return &runtimeinstall.Release{TagName: "v1", Assets: []runtimeinstall.Asset{
				{Name: "alpha", BrowserDownloadURL: "alpha://binary"},
			}}, nil
		},
		Download: func(ctx context.Context, url string) ([]byte, error) {
			if url != "alpha://binary" {
				return nil, fmt.Errorf("unexpected url %s", url)
			}
			return []byte("#!/bin/sh\n"), nil
		},
	}

	results, err := installRuntimesFor(ctx, opts, r, []string{"alpha-amd64"})
	if err != nil {
		t.Fatalf("installRuntimesFor: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one selected result, got %d: %v", len(results), results)
	}
	if results[0].Name != "alpha-amd64" || results[0].Err != nil {
		t.Fatalf("unexpected result: %+v", results[0])
	}
	if len(fetched) != 1 || fetched[0] != "alpha/repo" {
		t.Fatalf("selected install should fetch only alpha/repo, fetched %v", fetched)
	}
}

func TestInstallRuntimesForRejectsUnknownNamesBeforeInstall(t *testing.T) {
	ctx := context.Background()
	r := NewRegistryRaw()
	r.Register(&mockRuntimeProvider{
		mockPlugin: &mockPlugin{protocol: "alpha", displayName: "Alpha"},
		runtime: runtimeinstall.Runtime{
			Name:    "alpha",
			Binary:  "alpha",
			Method:  runtimeinstall.MethodRawBinary,
			Repo:    "alpha/repo",
			Version: "v1", Integrity: "pinned-sha256", PinnedSHA256: "a8076d3d28d21e02012b20eaf7dbf75409a6277134439025f282e368e3305abf",
			VersionArgs: []string{"--version"}, VersionCommand: "alpha --version", VersionPattern: `v1`,
			AssetMatch: func(name string) bool { return name == "alpha" },
		},
	})
	fetched := 0
	opts := runtimeinstall.Options{
		BinDir: t.TempDir(),
		Arch:   "amd64",
		FetchRelease: func(context.Context, string) (*runtimeinstall.Release, error) {
			fetched++
			return nil, fmt.Errorf("should not fetch")
		},
	}
	results, err := installRuntimesFor(ctx, opts, r, []string{"alpha-amd64", "hysetria2"})
	if err == nil {
		t.Fatalf("expected unknown-name error, got %+v", results)
	}
	if fetched != 0 {
		t.Fatalf("unknown names must not start any install, fetched=%d", fetched)
	}
	if !strings.Contains(err.Error(), "hysetria2") {
		t.Fatalf("error should name unknown runtime: %v", err)
	}
}
