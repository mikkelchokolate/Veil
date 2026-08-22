package api

import "testing"

func mustCreateSession(t *testing.T, registry *SessionRegistry, username, role string) Session {
	t.Helper()
	session, err := registry.NewSession(username, role)
	if err != nil {
		t.Fatal(err)
	}
	return session
}
