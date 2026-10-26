package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
	"github.com/mikkelchokolate/Veil/internal/releaseverify"
)

// TestPanelUpdateRefusesAPackageManagedBinary locks #1424 for the Panel button:
// the updater replaces only the executable, so it must refuse and point at the
// package manager instead of leaving the old package's units in place.
func TestPanelUpdateRefusesAPackageManagedBinary(t *testing.T) {
	stager := panelUpdateStager{
		root:      t.TempDir(),
		version:   "v0.7.2",
		assetName: updateflow.AssetName(),
		latest: func(context.Context) (*updateflow.Release, error) {
			t.Fatal("the stager must refuse before fetching a release")
			return nil, nil
		},
		download:      func(context.Context, string) ([]byte, error) { return nil, nil },
		verify:        func(releaseverify.Evidence) error { return nil },
		resolveCommit: func(context.Context, string) (string, error) { return strings.Repeat("a", 40), nil },
		packageManaged: func(string) (string, bool) {
			return "the Veil OS package", true
		},
	}

	_, err := stager.Stage(context.Background(), false)
	if err == nil {
		t.Fatal("panel updater accepted a package-managed binary")
	}
	var refused *updateRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("error %v is not an updateRefusedError (the handler maps that to 409)", err)
	}
	if !strings.Contains(err.Error(), "package manager") {
		t.Fatalf("error %v does not point at the package manager", err)
	}
}

// TestPanelUpdateStagerChecksOnlyWhenConfigured keeps nil (tests, legacy
// constructions) meaning "skip the package probe" so the version check runs.
func TestPanelUpdateStagerChecksOnlyWhenConfigured(t *testing.T) {
	stager := panelUpdateStager{
		root:      t.TempDir(),
		version:   "v0.7.2",
		assetName: updateflow.AssetName(),
		latest: func(context.Context) (*updateflow.Release, error) {
			return &updateflow.Release{TagName: "v0.7.2"}, nil
		},
		download:      func(context.Context, string) ([]byte, error) { return nil, nil },
		verify:        func(releaseverify.Evidence) error { return nil },
		resolveCommit: func(context.Context, string) (string, error) { return strings.Repeat("a", 40), nil },
	}
	if _, err := stager.Stage(context.Background(), false); err == nil || !strings.Contains(err.Error(), "already at or newer") {
		t.Fatalf("stager without the package hook: %v", err)
	}
}
