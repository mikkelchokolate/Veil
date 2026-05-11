package serve

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestRunServeLifecycleShutsDownOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer

	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux()}
	cancel()
	if err := RunLifecycle(LifecycleOptions{Context: ctx, Out: &out, Err: &out, Server: server}); err != nil {
		t.Fatalf("RunLifecycle: %v", err)
	}
	if !strings.Contains(out.String(), "Shutting down") || !strings.Contains(out.String(), "Server stopped") {
		t.Fatalf("shutdown output missing:\n%s", out.String())
	}
}
