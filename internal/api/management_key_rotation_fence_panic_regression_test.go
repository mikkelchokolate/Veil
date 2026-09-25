package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// panicRotateClient panics inside the fenced section of handleRotateKey —
// the failure mode from #1045.
type panicRotateClient struct {
	*recordingPrivilegedClient
}

func (c *panicRotateClient) RotateKey(context.Context, privileged.RotateKeyRequest) error {
	panic("injected panic inside fenced rotation")
}

// Regression for #1045: a panic between lease acquisition and the explicit
// release points left the durable apply lease owned by a live panel pid, so
// the dead-owner rescue never fired and every fenced mutation was blocked
// until the 2h TTL. The deferred release must free it during unwind.
func TestHandleRotateKeyReleasesFenceOnPanic(t *testing.T) {
	client := &panicRotateClient{recordingPrivilegedClient: &recordingPrivilegedClient{}}
	state := newFencedPanelState(t, client)
	admin, err := state.sessionRegistry().Create(SessionCreateInput{Username: "admin", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	request := adminJSONRequest(http.MethodPost, "/api/admin/rotate-key", `{}`)
	request.AddCookie(&http.Cookie{Name: "veil_session", Value: admin.Token})
	response := httptest.NewRecorder()

	panicked := func() (recovered bool) {
		defer func() { recovered = recover() != nil }()
		state.handleRotateKey(response, request)
		return false
	}()
	if !panicked {
		t.Fatal("expected the injected panic to propagate")
	}

	// The lease must be acquirable immediately — pre-fix it stayed held by
	// the panicking request's live-pid owner.
	token, release, err := state.acquireRuntimeFence("post-panic")
	if err != nil {
		t.Fatalf("fencing lease was not released on panic: %v", err)
	}
	defer release()
	if token.Owner == "" || token.Generation == 0 {
		t.Fatalf("post-panic lease is incomplete: %+v", token)
	}
}
