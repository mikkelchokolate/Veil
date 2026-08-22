// Package testguard provides a production-noop hook that test binaries arm to
// fail fast when code under test falls back to a production filesystem path
// (/etc/veil, /var/lib/veil, /usr/local/bin, /run/veil). Production builds
// never set the hook, so the checks are zero-cost no-ops in real deployments.
// This keeps the production binary free of any dependency on package testing
// while still making "unit test touched a production path" a hard failure.
package testguard

import (
	"strings"
	"sync"
)

var armedHook struct {
	sync.RWMutex
	fn func(path string)
}

// SetHookForTests arms the guard. It must only be called from test code
// (typically TestMain). Passing nil disarms it.
func SetHookForTests(f func(path string)) {
	armedHook.Lock()
	armedHook.fn = f
	armedHook.Unlock()
}

// CheckProductionPath invokes the armed hook (if any) with the production
// default path the caller is about to use. Call it exactly at the branch
// where code falls back to a production default location.
func CheckProductionPath(path string) {
	armedHook.RLock()
	f := armedHook.fn
	armedHook.RUnlock()
	if f != nil {
		f(path)
	}
}

// CheckPath checks a path used by a generic filesystem writer. Unlike
// CheckProductionPath, it only reports exact production roots or descendants;
// lookalike prefixes such as /etc/veil-test are intentionally allowed.
func CheckPath(path string) {
	for _, root := range []string{
		"/etc/veil",
		"/var/lib/veil",
		"/usr/local/bin/veil",
		"/run/veil",
	} {
		if path == root || strings.HasPrefix(path, root+"/") {
			CheckProductionPath(path)
			return
		}
	}
}
