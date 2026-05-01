package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestServeCommandRejectsInvalidListenAddress(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"serve", "--listen", "bad-address"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected invalid listen error")
	}
	if !strings.Contains(err.Error(), "listen address must be host:port") {
		t.Fatalf("unexpected error: %v", err)
	}
}
