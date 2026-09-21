package hostenv

import (
	"path/filepath"
	"runtime"
	"testing"
)

// clearVeilEnv blanks every environment input the resolvers consult so each
// case exercises exactly the channel it configures.
func clearVeilEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"VEIL_ETC_DIR", "VEIL_VAR_DIR", "VEIL_LIVE_ROOT", "VEIL_KEY_PATH", "VEIL_STATE_PATH"} {
		t.Setenv(key, "")
	}
}

func TestEtcDirResolutionPriority(t *testing.T) {
	customEtc := filepath.Join(string(filepath.Separator), "custom", "etc")
	customVar := filepath.Join(string(filepath.Separator), "custom", "var")

	tests := []struct {
		name      string
		etcDir    string
		liveRoot  string
		keyPath   string
		want      string
		wantIsDef bool
	}{
		{name: "packaged default", wantIsDef: true},
		{name: "VEIL_ETC_DIR wins", etcDir: customEtc, liveRoot: filepath.Join(customVar, "generated"), want: customEtc},
		{name: "VEIL_ETC_DIR cleaned", etcDir: customEtc + string(filepath.Separator), want: customEtc},
		{name: "VEIL_LIVE_ROOT parent", liveRoot: filepath.Join(customEtc, "generated"), want: customEtc},
		{name: "VEIL_KEY_PATH parent", keyPath: filepath.Join(customEtc, "state.key"), want: customEtc},
		{name: "LIVE_ROOT beats KEY_PATH", liveRoot: filepath.Join(customEtc, "generated"), keyPath: filepath.Join(customVar, "state.key"), want: customEtc},
		{name: "relative LIVE_ROOT falls through", liveRoot: "generated", keyPath: filepath.Join(customEtc, "state.key"), want: customEtc},
		{name: "whitespace values ignored", etcDir: "  ", liveRoot: " ", keyPath: "\t", wantIsDef: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearVeilEnv(t)
			if tc.etcDir != "" {
				t.Setenv("VEIL_ETC_DIR", tc.etcDir)
			}
			if tc.liveRoot != "" {
				t.Setenv("VEIL_LIVE_ROOT", tc.liveRoot)
			}
			if tc.keyPath != "" {
				t.Setenv("VEIL_KEY_PATH", tc.keyPath)
			}
			want := tc.want
			if tc.wantIsDef {
				want = DefaultEtcDir
			}
			if got := EtcDir(); got != want {
				t.Fatalf("EtcDir() = %q, want %q", got, want)
			}
		})
	}
}

func TestEtcDirRootLiveRootFallsThrough(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("filesystem-root path semantics are POSIX-specific")
	}
	clearVeilEnv(t)
	// Dir("/") == "/" — a live root at the filesystem root must not yield "/"
	// as the etc dir; resolution falls through to the packaged default.
	t.Setenv("VEIL_LIVE_ROOT", "/generated")
	if got := EtcDir(); got != DefaultEtcDir {
		t.Fatalf("EtcDir() = %q, want %q", got, DefaultEtcDir)
	}
}

func TestVarDirResolutionPriority(t *testing.T) {
	customVar := filepath.Join(string(filepath.Separator), "custom", "var")
	customEtc := filepath.Join(string(filepath.Separator), "custom", "etc")

	tests := []struct {
		name      string
		varDir    string
		statePath string
		want      string
		wantIsDef bool
	}{
		{name: "packaged default", wantIsDef: true},
		{name: "VEIL_VAR_DIR wins", varDir: customVar, statePath: filepath.Join(customEtc, "state.json"), want: customVar},
		{name: "VEIL_VAR_DIR cleaned", varDir: customVar + string(filepath.Separator), want: customVar},
		{name: "VEIL_STATE_PATH parent", statePath: filepath.Join(customVar, "state.json"), want: customVar},
		{name: "relative STATE_PATH falls through", statePath: "state.json", wantIsDef: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearVeilEnv(t)
			if tc.varDir != "" {
				t.Setenv("VEIL_VAR_DIR", tc.varDir)
			}
			if tc.statePath != "" {
				t.Setenv("VEIL_STATE_PATH", tc.statePath)
			}
			want := tc.want
			if tc.wantIsDef {
				want = DefaultVarDir
			}
			if got := VarDir(); got != want {
				t.Fatalf("VarDir() = %q, want %q", got, want)
			}
		})
	}
}
