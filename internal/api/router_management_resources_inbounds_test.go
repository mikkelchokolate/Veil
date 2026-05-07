package api

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var _router_management_resources_inbounds_deps = []any{
	bytes.Buffer{}, sha256.Sum256, tls.VersionTLS12, base64.StdEncoding, hex.EncodeToString, json.NewDecoder, errors.New, fmt.Sprintf, log.Printf, http.MethodGet, httptest.NewRecorder, os.ReadFile, filepath.Join, strings.Contains, testing.T{},
}

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
	seedHy2 := httptest.NewRecorder()
	r.ServeHTTP(seedHy2, httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"hysteria2","protocol":"hysteria2","transport":"udp","port":443,"enabled":true}`)))
	if seedHy2.Code != http.StatusCreated {
		t.Fatalf("seed hysteria2 expected 201, got %d: %s", seedHy2.Code, seedHy2.Body.String())
	}

	update := httptest.NewRecorder()
	r.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/api/inbounds/hysteria2", strings.NewReader(`{"protocol":"hysteria2","transport":"tcp","port":443,"enabled":true}`)))
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
