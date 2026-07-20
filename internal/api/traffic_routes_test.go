package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/client"
	"path/filepath"
)

// newTrafficRouter builds a router via composition so the managementState
// (Reloader) is available for direct sample injection.
func newTrafficRouter(t *testing.T) (http.Handler, *managementState) {
	t.Helper()
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := atomicfile.Write(statePath, []byte(`{"schemaVersion":4,"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com"}}`), 0o600, 0o700); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, rel := NewRouterComposition(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: dir}).Build()
	st, ok := rel.(*managementState)
	if !ok {
		t.Fatalf("reloader is not *managementState: %T", rel)
	}
	return r, st
}

func sampleFor(bindingID string, up, down int64) client.Sample {
	return client.Sample{BindingID: bindingID, UploadBytes: up, DownloadBytes: down, AtUnix: time.Now().Unix()}
}

func cancelAfterFirst() context.Context {
	// The timeout is a safety bound so the test never hangs; cancel when done.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	go func() {
		<-ctx.Done()
		cancel()
	}()
	return ctx
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// seedTrafficClient creates a client+binding and records one sample directly
// in the store.
func seedTrafficClient(t *testing.T, r http.Handler, st *managementState, name string, up, down int64) string {
	t.Helper()
	v1Request(t, r, http.MethodPost, "/api/inbounds", `{"name":"in-`+name+`","protocol":"hysteria2","transport":"udp","port":18443,"enabled":true,"protocolFields":{"domain":"vpn.example.com"}}`)
	w := v1Request(t, r, http.MethodPost, "/api/v1/clients", `{"name":"`+name+`","bindings":[{"inboundId":"in-`+name+`","credential":"pw"}]}`)
	var c map[string]any
	_ = json.NewDecoder(w.Body).Decode(&c)
	if nested, ok := c["client"].(map[string]any); ok {
		c = nested
	}
	id := c["id"].(string)
	// Query the binding ID directly from the DB via the state's repository.
	var bindingID string
	row := st.db.QueryRow(`SELECT id FROM client_bindings WHERE client_id=? LIMIT 1`, id)
	_ = row.Scan(&bindingID)
	if bindingID != "" {
		if err := st.trafficStore.RecordSample(sampleFor(bindingID, up, down)); err != nil {
			t.Fatalf("record sample: %v", err)
		}
	} else {
		t.Fatalf("no binding found for client %s", id)
	}
	return id
}

func TestV1TrafficTotalsAndDepleted(t *testing.T) {
	r, st := newTrafficRouter(t)
	id := seedTrafficClient(t, r, st, "alice", 600, 500)

	w := v1Request(t, r, http.MethodGet, "/api/v1/traffic/"+id, "")
	if w.Code != http.StatusOK {
		t.Fatalf("traffic: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		UploadBytes   int64 `json:"uploadBytes"`
		DownloadBytes int64 `json:"downloadBytes"`
		UsedBytes     int64 `json:"usedBytes"`
		Depleted      bool  `json:"depleted"`
	}
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body.UploadBytes != 600 || body.DownloadBytes != 500 || body.UsedBytes != 1100 {
		t.Fatalf("totals = %+v, want 600/500/1100", body)
	}
	if body.Depleted {
		t.Fatalf("no quota -> not depleted")
	}
}

func TestV1TrafficHistory(t *testing.T) {
	r, st := newTrafficRouter(t)
	id := seedTrafficClient(t, r, st, "bob", 100, 200)
	w := v1Request(t, r, http.MethodGet, "/api/v1/traffic/"+id+"/history", "")
	if w.Code != http.StatusOK {
		t.Fatalf("history: %d", w.Code)
	}
	var body struct {
		Count int `json:"count"`
	}
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body.Count < 1 {
		t.Fatalf("expected at least one history bucket, got %d", body.Count)
	}
}

func TestV1TrafficTopTalkers(t *testing.T) {
	r, st := newTrafficRouter(t)
	seedTrafficClient(t, r, st, "heavy", 9000, 9000)
	seedTrafficClient(t, r, st, "light", 10, 10)
	w := v1Request(t, r, http.MethodGet, "/api/v1/traffic/top?limit=10", "")
	if w.Code != http.StatusOK {
		t.Fatalf("top: %d", w.Code)
	}
	var body struct {
		Items []struct {
			Name      string `json:"name"`
			UsedBytes int64  `json:"usedBytes"`
		} `json:"items"`
	}
	_ = json.NewDecoder(w.Body).Decode(&body)
	if len(body.Items) < 2 {
		t.Fatalf("expected >=2 top talkers, got %+v", body.Items)
	}
	if body.Items[0].Name != "heavy" {
		t.Fatalf("top talker should be heavy first, got %+v", body.Items)
	}
}

func TestV1TrafficUnknownClient404(t *testing.T) {
	r, _ := newTrafficRouter(t)
	w := v1Request(t, r, http.MethodGet, "/api/v1/traffic/does-not-exist", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestV1TrafficStreamEmitsSnapshot(t *testing.T) {
	r, st := newTrafficRouter(t)
	seedTrafficClient(t, r, st, "streamer", 1, 2)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic/stream", nil)
	req = req.WithContext(cancelAfterFirst())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected event-stream, got %q", ct)
	}
	if got := w.Body.String(); got == "" || !contains(got, "event: traffic") {
		t.Fatalf("expected at least one traffic event, got %q", got)
	}
}
