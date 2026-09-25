package panelaccess

import (
	"bytes"
	"errors"
	"testing"
)

func TestWebBasePathPolicyGeneratesBase64URLPath(t *testing.T) {
	path, err := NewWebBasePathPolicy(bytes.NewReader([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9})).Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if path != "/AQIDBAUGBwgJ/" {
		t.Fatalf("path = %q", path)
	}
}

// TestWebBasePathPolicyFailsClosedWhenRandomFails is the #1022 regression: a
// crypto/rand failure must return an error, never the predictable
// "/veil-panel/" path.
func TestWebBasePathPolicyFailsClosedWhenRandomFails(t *testing.T) {
	path, err := NewWebBasePathPolicy(failingReader{}).Generate()
	if err == nil {
		t.Fatalf("expected error, got path %q", path)
	}
	if path == "/veil-panel/" {
		t.Fatalf("predictable-path fallback must never be returned")
	}
}

type failingReader struct{}

func (failingReader) Read(p []byte) (int, error) { return 0, errors.New("boom") }
