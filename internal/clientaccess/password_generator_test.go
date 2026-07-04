package clientaccess

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type errorReader struct{}

func (errorReader) Read(p []byte) (int, error) { return 0, errors.New("random failure") }

func TestManagementPasswordGeneratorProducesBase64URLString(t *testing.T) {
	g := NewManagementPasswordGenerator(nil)
	pass := g.Generate()
	if len(pass) != 12 {
		t.Fatalf("expected length 12, got %d: %q", len(pass), pass)
	}
	if strings.ContainsAny(pass, "+/=") {
		t.Fatalf("expected raw URL encoding, got %q", pass)
	}
}

func TestManagementPasswordGeneratorFallsBackOnReadError(t *testing.T) {
	g := NewManagementPasswordGenerator(errorReader{})
	if got := g.Generate(); got != "change-me" {
		t.Fatalf("got %q", got)
	}
}

func TestGenerateInboundPasswordProducesValue(t *testing.T) {
	pass := generateInboundPassword()
	if len(pass) != 12 {
		t.Fatalf("expected length 12, got %d: %q", len(pass), pass)
	}
}

var _ io.Reader = errorReader{}
