package testguard

import "testing"

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

	for _, path := range []string{
		"/etc/veil",
		"/etc/veil/generated/caddy/config.json",
		"/var/lib/veil/state.json",
		"/usr/local/bin/veil",
		"/run/veil/helper.sock",
	} {
		CheckPath(path)
	}
	if len(got) != 5 {
		t.Fatalf("production paths reported = %d, want 5: %v", len(got), got)
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
