package status

import (
	"os"
	"path/filepath"
	"testing"
)

func isolateListenConfig(t *testing.T) {
	t.Helper()
	t.Setenv("VEIL_LISTEN", "")
	t.Setenv("VEIL_STATE_PATH", "")
	origEnv, origState := installedEnvFile, installedStateFile
	missing := t.TempDir()
	installedEnvFile = filepath.Join(missing, "veil.env")
	installedStateFile = filepath.Join(missing, "state.json")
	t.Cleanup(func() {
		installedEnvFile, installedStateFile = origEnv, origState
	})
}

func TestResolveListenUsesVEILListenInsteadOfDefaultPort(t *testing.T) {
	isolateListenConfig(t)
	t.Setenv("VEIL_LISTEN", "127.0.0.1:47359")
	if got := ResolveListen(""); got != "127.0.0.1:47359" {
		t.Fatalf("ResolveListen(\"\") = %q, want installed/env address 127.0.0.1:47359", got)
	}
}

func TestResolveListenFlagOverridesEnvironment(t *testing.T) {
	isolateListenConfig(t)
	t.Setenv("VEIL_LISTEN", "127.0.0.1:47359")
	if got := ResolveListen("127.0.0.1:21100"); got != "127.0.0.1:21100" {
		t.Fatalf("ResolveListen(flag) = %q, want flag value", got)
	}
}

func TestResolveListenReadsInstalledEnvFile(t *testing.T) {
	isolateListenConfig(t)
	dir := t.TempDir()
	envPath := filepath.Join(dir, "veil.env")
	if err := os.WriteFile(envPath, []byte("VEIL_API_TOKEN=secret\nVEIL_LISTEN=127.0.0.1:47359\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installedEnvFile = envPath
	if got := ResolveListen(""); got != "127.0.0.1:47359" {
		t.Fatalf("ResolveListen(\"\") = %q, want address from veil.env", got)
	}
}

func TestResolveListenReadsInstalledStateWhenEnvFileMissingListen(t *testing.T) {
	isolateListenConfig(t)
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"settings":{"panelListen":"127.0.0.1:38412"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	installedStateFile = statePath
	if got := ResolveListen(""); got != "127.0.0.1:38412" {
		t.Fatalf("ResolveListen(\"\") = %q, want address from installed state", got)
	}
}

func TestResolveListenNormalizesWildcardBinds(t *testing.T) {
	isolateListenConfig(t)
	if got := ResolveListen("0.0.0.0:47359"); got != "127.0.0.1:47359" {
		t.Fatalf("ResolveListen(0.0.0.0) = %q, want loopback probe address", got)
	}
	t.Setenv("VEIL_LISTEN", "[::]:38412")
	if got := ResolveListen(""); got != "[::1]:38412" {
		t.Fatalf("ResolveListen(unspecified IPv6) = %q, want [::1]:38412", got)
	}
}

func TestResolveListenFallsBackWhenConfigMissing(t *testing.T) {
	isolateListenConfig(t)
	if got := ResolveListen(""); got != "127.0.0.1:2096" {
		t.Fatalf("ResolveListen(\"\") = %q, want default 127.0.0.1:2096", got)
	}
}

func TestResolveListenIgnoresUnreadableInstalledConfig(t *testing.T) {
	isolateListenConfig(t)
	dir := t.TempDir()
	envPath := filepath.Join(dir, "veil.env")
	if err := os.Mkdir(envPath, 0o700); err != nil {
		t.Fatal(err)
	}
	installedEnvFile = envPath
	if got := ResolveListen(""); got != "127.0.0.1:2096" {
		t.Fatalf("ResolveListen with unreadable env file = %q, want default", got)
	}
}
