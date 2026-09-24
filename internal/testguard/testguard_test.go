package testguard

import (
	"reflect"
	"testing"
)

func TestCheckPathOnlyReportsProductionLocations(t *testing.T) {
	var got []string
	SetHookForTests(func(path string) { got = append(got, path) })
	t.Cleanup(func() { SetHookForTests(nil) })

	for _, path := range []string{
		"/tmp/veil-test/state.json",
		"relative/generated/config.json",
		"/home/ci/workspace/state.json",
	} {
		CheckPath(path)
	}
	if len(got) != 0 {
		t.Fatalf("non-production paths triggered guard: %v", got)
	}

	wantPaths := []string{
		"/etc/veil",
		"/etc/veil/generated/caddy/config.json",
		"/var/lib/veil/state.json",
		"/usr/local/bin/veil",
		"/run/veil/helper.sock",
	}
	for _, path := range wantPaths {
		CheckPath(path)
	}
	// Lock the reported set itself, not just its length — a guard reporting
	// five WRONG paths would still green a len check (#898).
	if !reflect.DeepEqual(got, wantPaths) {
		t.Fatalf("production paths reported = %v, want %v", got, wantPaths)
	}
}

func TestCheckPathDoesNotMatchPrefixLookalikes(t *testing.T) {
	called := false
	SetHookForTests(func(string) { called = true })
	t.Cleanup(func() { SetHookForTests(nil) })

	for _, path := range []string{
		"/etc/veil-test/state.json",
		"/var/lib/veiled/state.json",
		"/usr/local/bin/veil-test",
		"/run/veiled/helper.sock",
	} {
		CheckPath(path)
	}
	if called {
		t.Fatal("production-path lookalike triggered guard")
	}
}
