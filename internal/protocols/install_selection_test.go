package protocols

import (
	"context"
	"fmt"
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
			Name:       "alpha",
			Binary:     "alpha",
			Method:     runtimeinstall.MethodRawBinary,
			Repo:       "alpha/repo",
			AssetMatch: func(name string) bool { return name == "alpha" },
		},
	})
	r.Register(&mockRuntimeProvider{
		mockPlugin: &mockPlugin{protocol: "beta", displayName: "Beta"},
		runtime: runtimeinstall.Runtime{
			Name:       "beta",
			Binary:     "beta",
			Method:     runtimeinstall.MethodRawBinary,
			Repo:       "beta/repo",
			AssetMatch: func(name string) bool { return name == "beta" },
		},
	})

	var fetched []string
	opts := runtimeinstall.Options{
		BinDir: binDir,
		Arch:   "amd64",
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

	results := installRuntimesFor(ctx, opts, r, []string{"alpha-amd64"})
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
