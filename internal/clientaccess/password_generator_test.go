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
	pass, err := g.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(pass) != 12 {
		t.Fatalf("expected length 12, got %d: %q", len(pass), pass)
	}
	if strings.ContainsAny(pass, "+/=") {
		t.Fatalf("expected raw URL encoding, got %q", pass)
	}
}

// TestManagementPasswordGeneratorFailsClosedOnReadError is the #1022
// regression: a crypto/rand failure must return an error, never the
// historical known "change-me" credential.
func TestManagementPasswordGeneratorFailsClosedOnReadError(t *testing.T) {
	g := NewManagementPasswordGenerator(errorReader{})
	got, err := g.Generate()
	if err == nil {
		t.Fatalf("expected error, got password %q", got)
	}
	if got == "change-me" {
		t.Fatalf("known-credential fallback must never be returned")
	}
}

func TestGenerateInboundPasswordProducesValue(t *testing.T) {
	pass, err := generateInboundPassword()
	if err != nil {
		t.Fatalf("generateInboundPassword: %v", err)
	}
	if len(pass) != 12 {
		t.Fatalf("expected length 12, got %d: %q", len(pass), pass)
	}
}

var _ io.Reader = errorReader{}
