package api

import (
	"fmt"
	"testing"
	"time"
)

// Overflow eviction must remove strictly-stale entries before touching any
// fresh one — a login spray can fill the map with dead (client, username)
// keys, and resetting a victim's live backoff would reset protection
// (#1222).
func TestLoginBackoffEvictsStaleEntriesBeforeFresh(t *testing.T) {
	now := time.Now()
	state := &managementState{
		loginBackoff:    make(map[string]loginBackoffState, loginBackoffMaxEntries),
		loginBackoffNow: func() time.Time { return now },
	}
	stale := loginBackoffState{failures: 9, lastSeen: now.Add(-loginBackoffStaleAfter - time.Minute)}
	for i := 0; i < loginBackoffMaxEntries-2; i++ {
		state.loginBackoff[fmt.Sprintf("stale-%d", i)] = stale
	}
	state.loginBackoff["fresh-old"] = loginBackoffState{failures: 3, lastSeen: now.Add(-time.Minute)}
	state.loginBackoff["fresh-new"] = loginBackoffState{failures: 3, lastSeen: now}

	state.recordLoginFailure("attacker")
	for _, key := range []string{"attacker", "fresh-old", "fresh-new"} {
		if _, ok := state.loginBackoff[key]; !ok {
			t.Fatalf("eviction removed live entry %q", key)
		}
	}
	if got := len(state.loginBackoff); got != 3 {
		t.Fatalf("stale entries survived eviction: len=%d", got)
	}
}

// When no stale entries exist the oldest lastSeen goes first — never a
// random victim, and never the key the call is recording (#1222).
func TestLoginBackoffEvictsOldestLastSeenWhenNothingStale(t *testing.T) {
	now := time.Now()
	state := &managementState{
		loginBackoff:    make(map[string]loginBackoffState, loginBackoffMaxEntries),
		loginBackoffNow: func() time.Time { return now },
	}
	state.loginBackoff["oldest"] = loginBackoffState{failures: 2, lastSeen: now.Add(-loginBackoffStaleAfter + time.Minute)}
	for i := 0; i < loginBackoffMaxEntries-2; i++ {
		state.loginBackoff[fmt.Sprintf("recent-%d", i)] = loginBackoffState{failures: 2, lastSeen: now.Add(-time.Duration(i) * time.Millisecond)}
	}
	state.loginBackoff["newest"] = loginBackoffState{failures: 2, lastSeen: now}

	state.recordLoginFailure("attacker")
	if _, ok := state.loginBackoff["oldest"]; ok {
		t.Fatal("the oldest entry survived a full-fresh overflow")
	}
	if _, ok := state.loginBackoff["attacker"]; !ok {
		t.Fatal("the in-flight key evicted itself")
	}
	if _, ok := state.loginBackoff["newest"]; !ok {
		t.Fatal("the newest entry was evicted before the oldest")
	}
	if got := len(state.loginBackoff); got != loginBackoffMaxEntries {
		t.Fatalf("map size=%d after eviction, want %d", got, loginBackoffMaxEntries)
	}
}
