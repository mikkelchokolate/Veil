package privileged

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

// Regression for #1008: the helper applied one fixed 30s deadline covering
// decode + dispatch + response, so a mutation or backup whose client budget
// is minutes/hours had its result silently discarded when it outlived 30s.
// Once the request is decoded the connection deadline must scale to that
// operation's budget. The test compresses only the pre-decode budget (50ms
// standing in for 30s) — a promote that runs past it must still get its
// response delivered under the real mutation budget.
func TestServerExtendsDeadlineToOperationBudget(t *testing.T) {
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{
		Promote: func(context.Context, ResolvedPromotion) (PromoteResult, error) {
			time.Sleep(200 * time.Millisecond)
			return PromoteResult{}, nil
		},
	}))
	server.timeout = 50 * time.Millisecond

	client, helper := net.Pipe()
	done := make(chan struct{})
	go func() {
		server.ServeConn(context.Background(), helper)
		close(done)
	}()
	request := RequestEnvelope{
		Version:   ProtocolVersion,
		RequestID: "long-promote",
		Operation: OperationPromote,
		Promote:   &PromoteRequest{ArtifactIDs: []string{"mieru"}},
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write(append(raw, '\n')); err != nil {
		t.Fatalf("write request: %v", err)
	}
	var response ResponseEnvelope
	if err := json.NewDecoder(bufio.NewReader(client)).Decode(&response); err != nil {
		t.Fatalf("response for a long-running promote was discarded: %v", err)
	}
	if !response.OK || response.Error != nil {
		t.Fatalf("unexpected response: %+v", response)
	}
	client.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not close after one response")
	}
}

// #1008 companion: the wider per-operation budget never overrides a caller
// context that expires first — the earlier deadline still wins.
func TestServerContextDeadlineStillBoundsOperation(t *testing.T) {
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{
		Promote: func(context.Context, ResolvedPromotion) (PromoteResult, error) {
			time.Sleep(500 * time.Millisecond)
			return PromoteResult{}, nil
		},
	}))
	server.timeout = 50 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	client, helper := net.Pipe()
	done := make(chan struct{})
	go func() {
		server.ServeConn(ctx, helper)
		close(done)
	}()
	request := RequestEnvelope{
		Version:   ProtocolVersion,
		RequestID: "ctx-bound-promote",
		Operation: OperationPromote,
		Promote:   &PromoteRequest{ArtifactIDs: []string{"mieru"}},
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write(append(raw, '\n')); err != nil {
		t.Fatalf("write request: %v", err)
	}
	var response ResponseEnvelope
	if err := json.NewDecoder(bufio.NewReader(client)).Decode(&response); err == nil && response.OK {
		t.Fatal("response arrived after the context deadline expired")
	}
	client.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not close after context deadline")
	}
}

// #1008: client and server must derive the same budget for every operation —
// a drift means the helper discards a result the caller still waits for.
func TestServerAndClientShareOperationBudgetClassification(t *testing.T) {
	server := NewServer(nil)
	server.timeout = 30 * time.Second
	server.mutationLimit = 15 * time.Minute
	server.backupLimit = 2 * time.Hour
	client := NewSocketClient("/nonexistent.sock")
	for _, operation := range operationConstants(t) {
		if got, want := server.operationTimeout(operation), client.operationTimeout(operation); got != want {
			t.Fatalf("operation %s: server budget %v != client budget %v", operation, got, want)
		}
	}
	// And the classification must actually differentiate: a fixed budget for
	// every operation is what caused #1008.
	if server.operationTimeout(OperationPromote) <= server.operationTimeout(OperationJournal) {
		t.Fatal("mutation operations did not get a wider budget than status probes")
	}
	if server.operationTimeout(OperationBackupRestore) <= server.operationTimeout(OperationPromote) {
		t.Fatal("backup operations did not get the widest budget")
	}
}
