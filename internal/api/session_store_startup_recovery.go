package api

import "time"

// newSessionRegistryWithoutLoad preserves the configured persistence path after
// startup load failures. Existing sessions remain invalidated, but subsequent
// creates and revocations must still succeed or fail against the real store
// instead of silently degrading to an in-memory registry.
func newSessionRegistryWithoutLoad(path string) *SessionRegistry {
	return &SessionRegistry{
		path:            path,
		sessions:        make(map[string]storedSession),
		rawCSRF:         make(map[string]string),
		now:             time.Now,
		idleTimeout:     defaultSessionIdleTimeout,
		absoluteTimeout: defaultSessionAbsoluteTimeout,
	}
}
