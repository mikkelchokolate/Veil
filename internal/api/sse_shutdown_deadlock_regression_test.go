package api

import (
	"testing"
	"time"
)

func TestManagementStateCloseJoinsSSEWithoutRequestWriteLock(t *testing.T) {
	state := newClientLifecycleTestState(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	sseRefreshGate = func() {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
	}
	t.Cleanup(func() {
		sseRefreshGate = nil
		select {
		case <-release:
		default:
			close(release)
		}
	})

	state.mu.Lock()
	state.sse = newSSEBroadcaster(state)
	state.mu.Unlock()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE refresh did not reach the request-lock gate")
	}

	done := make(chan error, 1)
	go func() { done <- state.Close() }()
	select {
	case err := <-done:
		t.Fatalf("Close returned before the in-flight refresh resumed: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close deadlocked joining SSE refresh")
	}
}

func TestManagementStateCloseWithoutSSEBroadcaster(t *testing.T) {
	state := newClientLifecycleTestState(t)
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}
}
