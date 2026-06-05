package privileged

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestServerHandlesOneJSONRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{
		ServiceAction: func(_ context.Context, request ServiceActionRequest) error {
			calls.Add(1)
			if request.Unit != "veil.service" || request.Action != ServiceActionRestart {
				t.Fatalf("unexpected service action: %+v", request)
			}
			return nil
		},
	}))
	request := RequestEnvelope{
		Version:       ProtocolVersion,
		RequestID:     "round-trip",
		Operation:     OperationServiceAction,
		ServiceAction: &ServiceActionRequest{Unit: "veil.service", Action: ServiceActionRestart},
	}
	response := servePipeRequest(t, server, request)
	if !response.OK || response.Error != nil {
		t.Fatalf("unexpected response: %+v", response)
	}
	if response.Version != ProtocolVersion || response.RequestID != request.RequestID {
		t.Fatalf("response correlation mismatch: %+v", response)
	}
	if calls.Load() != 1 {
		t.Fatalf("want one executor call, got %d", calls.Load())
	}
}

func TestServerRejectsProtocolMismatchAndUnknownFields(t *testing.T) {
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{}))
	tests := map[string]string{
		"version": `{"version":99,"requestId":"bad-version","operation":"restart_panel","restartPanel":{}}`,
		"unknown": `{"version":1,"requestId":"unknown","operation":"restart_panel","restartPanel":{},"command":"reboot"}`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			response := servePipeRaw(t, server, raw)
			if response.OK || response.Error == nil || response.Error.Code != ErrorInvalidRequest {
				t.Fatalf("expected invalid_request response, got %+v", response)
			}
		})
	}
}

func TestServerRejectsMultiplePayloadsBeforeExecution(t *testing.T) {
	var calls atomic.Int32
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{
		RestartPanel: func(context.Context) error {
			calls.Add(1)
			return nil
		},
	}))
	raw := `{"version":1,"requestId":"multi","operation":"restart_panel","restartPanel":{},"journal":{"unit":"veil.service","lines":10}}`
	response := servePipeRaw(t, server, raw)
	if response.OK || response.Error == nil || response.Error.Code != ErrorInvalidRequest {
		t.Fatalf("expected invalid_request response, got %+v", response)
	}
	if calls.Load() != 0 {
		t.Fatalf("executor called %d times", calls.Load())
	}
}

func TestServerRejectsRequestsLargerThanOneMiB(t *testing.T) {
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{}))
	raw := `{"version":1,"requestId":"` + strings.Repeat("x", (1<<20)+1) + `","operation":"restart_panel","restartPanel":{}}`
	response := servePipeRaw(t, server, raw)
	if response.OK || response.Error == nil || response.Error.Code != ErrorInvalidRequest {
		t.Fatalf("expected invalid_request response, got %+v", response)
	}
}

func TestServerRejectsTwoRequestsOnOneConnection(t *testing.T) {
	var calls atomic.Int32
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{
		RestartPanel: func(context.Context) error {
			calls.Add(1)
			return nil
		},
	}))
	raw := `{"version":1,"requestId":"first","operation":"restart_panel","restartPanel":{}}` +
		`{"version":1,"requestId":"second","operation":"restart_panel","restartPanel":{}}`
	response := servePipeRaw(t, server, raw)
	if response.OK || response.Error == nil || response.Error.Code != ErrorInvalidRequest {
		t.Fatalf("expected invalid_request response, got %+v", response)
	}
	if calls.Load() != 0 {
		t.Fatalf("executor called before multi-request rejection: %d", calls.Load())
	}
}

func TestServerHonorsContextDeadline(t *testing.T) {
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{}))
	client, helper := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		server.ServeConn(ctx, helper)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeConn ignored context deadline")
	}
}

func TestValidateSocketPathRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.sock")
	link := filepath.Join(root, "helper.sock")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if err := validateSocketPath(link); err == nil {
		t.Fatal("expected symlink socket path rejection")
	}
}

func servePipeRequest(t *testing.T, server *Server, request RequestEnvelope) ResponseEnvelope {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return servePipeRaw(t, server, string(raw))
}

func servePipeRaw(t *testing.T, server *Server, raw string) ResponseEnvelope {
	t.Helper()
	client, helper := net.Pipe()
	done := make(chan struct{})
	go func() {
		server.ServeConn(context.Background(), helper)
		close(done)
	}()
	writeDone := make(chan error, 1)
	go func() {
		_, err := client.Write([]byte(raw + "\n"))
		writeDone <- err
	}()
	var response ResponseEnvelope
	if err := json.NewDecoder(bufio.NewReader(client)).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	client.Close()
	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("request writer did not finish")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server did not close after one response")
	}
	return response
}
