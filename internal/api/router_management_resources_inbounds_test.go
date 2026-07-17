package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

func TestManagementAPICreatesInbound(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	body := strings.NewReader(`{"name":"hy2-alt","protocol":"hysteria2","transport":"udp","port":8443,"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var response Inbound
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Name != "hy2-alt" || response.Port != 8443 {
		t.Fatalf("unexpected inbound: %+v", response)
	}
	if ct := w.Result().Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("expected JSON content-type on 201, got %q", ct)
	}
	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected no-store cache-control on 201, got %q", cc)
	}
	if nosniff := w.Result().Header.Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("expected nosniff on 201, got %q", nosniff)
	}
}

func TestManagementAPIInboundsRejectOversizedJSONBodies(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	seed := httptest.NewRecorder()
	r.ServeHTTP(seed, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true}`)))
	if seed.Code != http.StatusCreated {
		t.Fatalf("seed inbound expected 201, got %d: %s", seed.Code, seed.Body.String())
	}
	oversizedName := strings.Repeat("a", 1024*1024+1)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "create",
			method: http.MethodPost,
			path:   "/api/inbounds",
			body:   `{"name":"` + oversizedName + `","protocol":"hysteria2","transport":"udp","port":8443,"enabled":true}`,
		},
		{
			name:   "update",
			method: http.MethodPut,
			path:   "/api/inbounds/naive",
			body:   `{"protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true,"path":"` + oversizedName + `"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			if w.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("expected 413 for oversized inbound body, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestManagementAPIRejectsDuplicateInboundName(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	seed := httptest.NewRecorder()
	r.ServeHTTP(seed, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true}`)))
	if seed.Code != http.StatusCreated {
		t.Fatalf("seed inbound expected 201, got %d: %s", seed.Code, seed.Body.String())
	}
	body := strings.NewReader(`{"name":"naive","protocol":"naiveproxy","transport":"tcp","port":8443,"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 duplicate inbound name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestManagementAPIRejectsDuplicateInboundTransportPort(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	seed := httptest.NewRecorder()
	r.ServeHTTP(seed, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true}`)))
	if seed.Code != http.StatusCreated {
		t.Fatalf("seed inbound expected 201, got %d: %s", seed.Code, seed.Body.String())
	}
	body := strings.NewReader(`{"name":"duplicate-naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 duplicate transport/port, got %d: %s", w.Code, w.Body.String())
	}
}

func TestManagementAPIUpdatesAndDeletesInboundByName(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	create := httptest.NewRecorder()
	r.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"hy2-alt","protocol":"hysteria2","transport":"udp","port":8443,"enabled":true}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create inbound expected 201, got %d: %s", create.Code, create.Body.String())
	}

	update := httptest.NewRecorder()
	r.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/api/inbounds/hy2-alt", strings.NewReader(`{"protocol":"hysteria2","transport":"udp","port":9443,"enabled":false}`)))
	if update.Code != http.StatusOK {
		t.Fatalf("update inbound expected 200, got %d: %s", update.Code, update.Body.String())
	}
	var updated Inbound
	if err := json.NewDecoder(update.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated inbound: %v", err)
	}
	if updated.Name != "hy2-alt" || updated.Port != 9443 || updated.Enabled {
		t.Fatalf("unexpected updated inbound: %+v", updated)
	}

	restarted, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	readAfterUpdate := httptest.NewRecorder()
	restarted.ServeHTTP(readAfterUpdate, httptest.NewRequest(http.MethodGet, "/api/inbounds", nil))
	if !strings.Contains(readAfterUpdate.Body.String(), `"port":9443`) || strings.Contains(readAfterUpdate.Body.String(), `"port":8443`) {
		t.Fatalf("persisted inbound update missing: %s", readAfterUpdate.Body.String())
	}

	deleteRecorder := httptest.NewRecorder()
	restarted.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete, "/api/inbounds/hy2-alt", nil))
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete inbound expected 204, got %d: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	readAfterDelete := httptest.NewRecorder()
	restarted.ServeHTTP(readAfterDelete, httptest.NewRequest(http.MethodGet, "/api/inbounds", nil))
	if strings.Contains(readAfterDelete.Body.String(), "hy2-alt") {
		t.Fatalf("deleted inbound still present: %s", readAfterDelete.Body.String())
	}
}

func TestManagementAPIRejectsInboundUpdateToDuplicateTransportPort(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})
	seedNaive := httptest.NewRecorder()
	r.ServeHTTP(seedNaive, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"naive","protocol":"naiveproxy","transport":"tcp","port":443,"enabled":true}`)))
	if seedNaive.Code != http.StatusCreated {
		t.Fatalf("seed naive expected 201, got %d: %s", seedNaive.Code, seedNaive.Body.String())
	}
	seedMieru := httptest.NewRecorder()
	r.ServeHTTP(seedMieru, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"mieru","protocol":"mieru","transport":"udp","port":8443,"enabled":true}`)))
	if seedMieru.Code != http.StatusCreated {
		t.Fatalf("seed mieru expected 201, got %d: %s", seedMieru.Code, seedMieru.Body.String())
	}

	update := httptest.NewRecorder()
	r.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/api/inbounds/mieru", strings.NewReader(`{"protocol":"mieru","transport":"tcp","port":443,"enabled":true}`)))
	if update.Code != http.StatusConflict {
		t.Fatalf("expected 409 duplicate transport/port on update, got %d: %s", update.Code, update.Body.String())
	}
}

func TestManagementAPIGetsInboundByName(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	create := httptest.NewRecorder()
	r.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"hy2-alt","protocol":"hysteria2","transport":"udp","port":8443,"enabled":true}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create inbound expected 201, got %d: %s", create.Code, create.Body.String())
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/inbounds/hy2-alt", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET named inbound, got %d: %s", w.Code, w.Body.String())
	}
	var response Inbound
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Name != "hy2-alt" || response.Port != 8443 || response.Protocol != "hysteria2" {
		t.Fatalf("unexpected inbound: %+v", response)
	}
}

func TestManagementAPIGetInboundByNameReturnsNotFoundForMissing(t *testing.T) {
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev"})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/inbounds/nonexistent", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing inbound, got %d: %s", w.Code, w.Body.String())
	}
}

func TestManagementAPIInboundProtocolOverrides(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	r, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	payload := `{
		"name": "naive-overridden",
		"protocol": "naiveproxy",
		"transport": "tcp",
		"port": 1443,
		"enabled": true,
		"naiveUsername": "custom-naive-user",
		"naivePassword": "custom-naive-password",
		"fallbackRoot": "/custom/fallback/root",
		"hysteria2Password": "custom-hy2-pass",
		"masqueradeURL": "https://custom-masquerade.com",
		"olcrtcAuth": "livekit",
		"olcrtcTransport": "websocket",
		"olcrtcRoomID": "custom-livekit-room-12345"
	}`

	create := httptest.NewRecorder()
	r.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(payload)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create inbound expected 201, got %d: %s", create.Code, create.Body.String())
	}

	var response Inbound
	if err := json.NewDecoder(create.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.NaiveUsername != "custom-naive-user" ||
		response.NaivePassword != veilsettings.RedactedSecret ||
		response.FallbackRoot != "/custom/fallback/root" ||
		response.Hysteria2Password != veilsettings.RedactedSecret ||
		response.MasqueradeURL != "https://custom-masquerade.com" ||
		response.OlcrtcAuth != "livekit" ||
		response.OlcrtcTransport != "websocket" ||
		response.OlcrtcRoomID != "custom-livekit-room-12345" {
		t.Fatalf("unexpected parsed overrides in response: %+v", response)
	}

	// Reload state from path and verify persistence
	restarted, _ := NewRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})
	readRecorder := httptest.NewRecorder()
	restarted.ServeHTTP(readRecorder, httptest.NewRequest(http.MethodGet, "/api/inbounds/naive-overridden", nil))

	if readRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", readRecorder.Code, readRecorder.Body.String())
	}

	var reloaded Inbound
	if err := json.NewDecoder(readRecorder.Body).Decode(&reloaded); err != nil {
		t.Fatalf("decode reloaded inbound: %v", err)
	}

	if reloaded.NaiveUsername != "custom-naive-user" ||
		reloaded.NaivePassword != veilsettings.RedactedSecret ||
		reloaded.FallbackRoot != "/custom/fallback/root" ||
		reloaded.Hysteria2Password != veilsettings.RedactedSecret ||
		reloaded.MasqueradeURL != "https://custom-masquerade.com" ||
		reloaded.OlcrtcAuth != "livekit" ||
		reloaded.OlcrtcTransport != "websocket" ||
		reloaded.OlcrtcRoomID != "custom-livekit-room-12345" {
		t.Fatalf("unexpected persisted overrides: %+v", reloaded)
	}
}
