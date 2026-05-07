package installer

import (
	"bytes"
	"errors"
	"testing"
)

func TestWebBasePathPolicyGeneratesBase64URLPath(t *testing.T) {
	path := NewWebBasePathPolicy(bytes.NewReader([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9})).Generate()
	if path != "/AQIDBAUGBwgJ/" {
		t.Fatalf("path = %q", path)
	}
}

func TestWebBasePathPolicyFallsBackWhenRandomFails(t *testing.T) {
	path := NewWebBasePathPolicy(failingReader{}).Generate()
	if path != "/veil-panel/" {
		t.Fatalf("path = %q", path)
	}
}

type failingReader struct{}

func (failingReader) Read(p []byte) (int, error) { return 0, errors.New("boom") }
