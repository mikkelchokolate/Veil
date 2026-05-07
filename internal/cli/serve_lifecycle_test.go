package cli

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestRunServeLifecycleShutsDownOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := NewRootCommand("test")
	cmd.SetContext(ctx)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	cancel()
	if err := runServeLifecycle(cmd, server, nil, false, "", ""); err != nil {
		t.Fatalf("runServeLifecycle: %v", err)
	}
	if !strings.Contains(out.String(), "Shutting down") || !strings.Contains(out.String(), "Server stopped") {
		t.Fatalf("shutdown output missing:\n%s", out.String())
	}
}
