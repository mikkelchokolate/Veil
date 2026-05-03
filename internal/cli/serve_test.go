package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
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

func TestServeCommandRejectsInvalidPortWithAuthToken(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"serve", "--listen", "localhost:notaport", "--auth-token", "secret"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected invalid port error when auth token is set")
	}
	if !strings.Contains(err.Error(), "invalid port") && !strings.Contains(err.Error(), "listen address") {
		t.Fatalf("expected error to mention invalid port or listen address, got: %v", err)
	}
}

func TestServeCommandRejectsEmptyHost(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"serve", "--listen", ":2096", "--auth-token", "secret"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected empty host error")
	}
	if !strings.Contains(err.Error(), "host") && !strings.Contains(err.Error(), "listen address") {
		t.Fatalf("expected error to mention host or listen address, got: %v", err)
	}
}

func TestServeGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"serve", "--listen", "127.0.0.1:12096", "--auth-token", "test-token"})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	// Wait for the server to start accepting connections.
	select {
	case err := <-errCh:
		t.Fatalf("server exited before shutdown signal: %v", err)
	case <-time.After(500 * time.Millisecond):
	}

	// Cancel the context to trigger graceful shutdown.
	cancel()

	// The server should shut down within the drain timeout plus a margin.
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected nil error after graceful shutdown, got: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not shut down within expected time")
	}
}
