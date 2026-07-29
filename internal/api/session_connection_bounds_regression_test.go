package api

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionRegistryBoundsAndPersistsDeterministicEviction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Truncate(time.Second)
	registry.now = func() time.Time { return base }
	var first, newest Session
	for index := 0; index < 1025; index++ {
		base = base.Add(time.Second)
		session, err := registry.Create(SessionCreateInput{Username: fmt.Sprintf("user-%04d", index), Role: "viewer"})
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = session
		}
		newest = session
	}
	if got := len(registry.List(newest.Token)); got != 1024 {
		t.Fatalf("active sessions=%d want bounded 1024", got)
	}
	if _, ok := registry.Get(first.Token); ok {
		t.Fatal("oldest session was not deterministically evicted at capacity")
	}
	if _, ok := registry.Get(newest.Token); !ok {
		t.Fatal("newest session was evicted instead of oldest")
	}

	restarted, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = registry.now
	if got := len(restarted.List(newest.Token)); got != 1024 {
		t.Fatalf("restarted active sessions=%d want 1024", got)
	}
	if _, ok := restarted.Get(first.Token); ok {
		t.Fatal("evicted session resurrected after restart")
	}
	if _, ok := restarted.Get(newest.Token); !ok {
		t.Fatal("newest session missing after restart")
	}
}

func TestSSEBroadcasterEnforcesGlobalConnectionBound(t *testing.T) {
	hub := newSSEBroadcaster(&managementState{})
	defer hub.Close()
	cleanups := make([]func(), 0, 1024)
	defer func() {
		for _, cleanup := range cleanups {
			cleanup()
		}
	}()
	for index := 0; index < 1024; index++ {
		_, cleanup, err := hub.subscribe(fmt.Sprintf("identity-%04d", index))
		if err != nil {
			t.Fatalf("subscription %d below global bound: %v", index, err)
		}
		cleanups = append(cleanups, cleanup)
	}
	if _, _, err := hub.subscribe("overflow-identity"); err == nil {
		t.Fatal("1025th global SSE connection was accepted")
	}
}
