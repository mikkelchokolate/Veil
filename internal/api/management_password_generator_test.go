package api

import (
	"errors"
	"strings"
	"testing"
)

func TestManagementPasswordGeneratorBuildsBase64URLPassword(t *testing.T) {
	password := NewManagementPasswordGenerator(strings.NewReader(string([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9}))).Generate()
	if password != "AQIDBAUGBwgJ" {
		t.Fatalf("password = %q", password)
	}
}

func TestManagementPasswordGeneratorFallsBackWhenRandomFails(t *testing.T) {
	password := NewManagementPasswordGenerator(errorReader{}).Generate()
	if password != "change-me" {
		t.Fatalf("password = %q", password)
	}
}

type errorReader struct{}

func (errorReader) Read(p []byte) (int, error) { return 0, errors.New("boom") }
