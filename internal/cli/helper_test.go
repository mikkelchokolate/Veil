package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelperCommandIsHiddenButServeHelpIsAvailable(t *testing.T) {
	root := NewRootCommand("test")
	helper, _, err := root.Find([]string{"helper"})
	if err != nil {
		t.Fatalf("find helper command: %v", err)
	}
	if !helper.Hidden {
		t.Fatal("helper command must be hidden from normal workflow help")
	}
	serve, _, err := root.Find([]string{"helper", "serve"})
	if err != nil {
		t.Fatalf("find helper serve command: %v", err)
	}
	if serve.Use != "serve" {
		t.Fatalf("unexpected helper subcommand: %s", serve.Use)
	}
}

func TestHelperServeRejectsRelativeSocketPath(t *testing.T) {
	cmd := newHelperCommandWithDependencies(helperCommandDependencies{
		GOOS:         "linux",
		EffectiveUID: func() int { return 0 },
		LookupUID:    func(string) (uint32, error) { return 1000, nil },
		Serve:        func(context.Context, string, uint32, bool) error { return nil },
	})
	cmd.SetArgs([]string{"serve", "--socket", "helper.sock"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("expected absolute path error, got %v", err)
	}
}

func TestHelperServeRejectsNonRootOnLinux(t *testing.T) {
	cmd := newHelperCommandWithDependencies(helperCommandDependencies{
		GOOS:         "linux",
		EffectiveUID: func() int { return 1000 },
		LookupUID:    func(string) (uint32, error) { return 1000, nil },
		Serve:        func(context.Context, string, uint32, bool) error { return nil },
	})
	cmd.SetArgs([]string{"serve", "--socket", filepath.Join(t.TempDir(), "helper.sock")})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("expected root requirement, got %v", err)
	}
}

func TestHelperServeResolvesVeilUIDAndStartsSocketServer(t *testing.T) {
	var gotPath string
	var gotUID uint32
	cmd := newHelperCommandWithDependencies(helperCommandDependencies{
		GOOS:         "linux",
		EffectiveUID: func() int { return 0 },
		LookupUID: func(name string) (uint32, error) {
			if name != "veil" {
				t.Fatalf("unexpected account lookup: %s", name)
			}
			return 4242, nil
		},
		Serve: func(_ context.Context, path string, uid uint32, allowRoot bool) error {
			gotPath = path
			gotUID = uid
			if allowRoot {
				t.Fatal("production helper must not allow root peers")
			}
			return errors.New("stop")
		},
	})
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	socketPath := filepath.Join(t.TempDir(), "helper.sock")
	cmd.SetArgs([]string{"serve", "--socket", socketPath})
	if err := cmd.Execute(); err == nil || err.Error() != "stop" {
		t.Fatalf("expected injected stop, got %v", err)
	}
	if gotPath != socketPath || gotUID != 4242 {
		t.Fatalf("serve arguments: path=%q uid=%d", gotPath, gotUID)
	}
}

func TestHelperServeUsesSystemdSocketActivation(t *testing.T) {
	var activated bool
	cmd := newHelperCommandWithDependencies(helperCommandDependencies{
		GOOS:         "linux",
		EffectiveUID: func() int { return 0 },
		LookupUID:    func(string) (uint32, error) { return 4242, nil },
		Serve: func(context.Context, string, uint32, bool) error {
			t.Fatal("path listener must not be used with socket activation")
			return nil
		},
		ServeActivated: func(_ context.Context, uid uint32, allowRoot bool) error {
			activated = true
			if uid != 4242 || allowRoot {
				t.Fatalf("activated arguments: uid=%d allowRoot=%t", uid, allowRoot)
			}
			return errors.New("stop")
		},
	})
	cmd.SetArgs([]string{"serve", "--systemd-socket-activation"})
	if err := cmd.Execute(); err == nil || err.Error() != "stop" {
		t.Fatalf("expected injected stop, got %v", err)
	}
	if !activated {
		t.Fatal("systemd socket listener was not used")
	}
}
