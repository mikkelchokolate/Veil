package veilclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// End-to-end round trip through the generated ClientWithResponses: request
// construction, client-level and per-call editors, transport, and typed
// response parsing for both the 200 body and the 401 body + header.
func TestGeneratedClientStatusRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/base/api/status" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Call") != "per-call" {
			t.Errorf("per-call request editor was not applied")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-Veil-Token") != "good" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="veil"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"token required"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"mode":"server","name":"veil","schemaVersion":"1","services":[],"version":"1.2.3"}`))
	}))
	defer server.Close()

	perCall := func(_ context.Context, r *http.Request) error {
		r.Header.Set("X-Call", "per-call")
		return nil
	}

	authed, err := NewClientWithResponses(server.URL+"/base/", WithRequestEditorFn(func(_ context.Context, r *http.Request) error {
		r.Header.Set("X-Veil-Token", "good")
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	ok, err := authed.GetApiStatusWithResponse(context.Background(), perCall)
	if err != nil {
		t.Fatal(err)
	}
	if ok.HTTPResponse.StatusCode != http.StatusOK || ok.JSON200 == nil || ok.JSON401 != nil {
		t.Fatalf("status=%d json200=%v json401=%v", ok.HTTPResponse.StatusCode, ok.JSON200, ok.JSON401)
	}
	if ok.JSON200.Version != "1.2.3" || ok.JSON200.Mode != "server" || ok.JSON200.Name != "veil" {
		t.Fatalf("unexpected status body %+v", *ok.JSON200)
	}

	anon, err := NewClientWithResponses(server.URL + "/base/")
	if err != nil {
		t.Fatal(err)
	}
	denied, err := anon.GetApiStatusWithResponse(context.Background(), perCall)
	if err != nil {
		t.Fatal(err)
	}
	if denied.HTTPResponse.StatusCode != http.StatusUnauthorized || denied.JSON200 != nil || denied.JSON401 == nil {
		t.Fatalf("status=%d json200=%v json401=%v", denied.HTTPResponse.StatusCode, denied.JSON200, denied.JSON401)
	}
	if denied.Headers401 == nil || denied.Headers401.WWWAuthenticate == nil || *denied.Headers401.WWWAuthenticate != `Bearer realm="veil"` {
		t.Fatalf("WWW-Authenticate header not parsed: %+v", denied.Headers401)
	}
	if denied.JSON401.Error.Code != "unauthorized" {
		t.Fatalf("unexpected 401 body %+v", *denied.JSON401)
	}
}
