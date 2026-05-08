package cli

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

var _installTestDeps_misc = []any{
	bytes.Buffer{}, net.ParseIP, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{}, installer.StackBoth,
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
