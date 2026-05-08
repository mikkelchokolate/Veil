package cli

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

var _installTestDeps_misc = []any{
	bytes.Buffer{}, net.ParseIP, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{}, installer.RURecommendedProfile{},
}

func TestRURecommendedInstallOptionsDoesNotExposeSharedProxyPort(t *testing.T) {
	if _, ok := reflect.TypeOf(ruRecommendedInstallOptions{}).FieldByName("SharedPort"); ok {
		t.Fatal("ruRecommendedInstallOptions should not expose shared proxy port planning")
	}
}

func TestInstallHelpHidesLegacyStackAndPortFlags(t *testing.T) {
	cmd := newInstallCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	for _, unwanted := range []string{"--stack", "--port", "--hysteria-sha256"} {
		if strings.Contains(out.String(), unwanted) {
			t.Fatalf("install help should hide legacy flag %q:\n%s", unwanted, out.String())
		}
	}
}

func TestRandomSecretGeneratesBase64String(t *testing.T) {
	// Test that randomSecret produces consistent-length base64 output.
	s := randomSecret("test-label")
	if len(s) == 0 {
		t.Fatal("expected non-empty random secret")
	}
	// base64.RawURLEncoding of 18 bytes = 24 chars
	if len(s) != 24 {
		t.Errorf("expected 24-char base64 string, got %d chars: %q", len(s), s)
	}
	// Calling again should produce a different value
	s2 := randomSecret("test-label")
	if s == s2 {
		t.Error("expected different random secrets")
	}
}
