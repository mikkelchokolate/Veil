package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/inbounds"
)

func TestHandleProtocols(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev"})
	req := httptest.NewRequest(http.MethodGet, "/api/protocols", nil)
	rec := httptest.NewRecorder()
	state.handleProtocols(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var protocols []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&protocols); err != nil {
		t.Fatal(err)
	}
	if len(protocols) == 0 {
		t.Fatal("expected protocols")
	}
}

func TestWriteInboundManagementError(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantBody   string
	}{
		{inbounds.ErrInboundInvalid, http.StatusBadRequest, "name, protocol, transport"},
		{inbounds.ErrInboundDuplicateName, http.StatusConflict, "inbound name already exists"},
		{inbounds.ErrInboundDuplicateTransportPort, http.StatusConflict, "transport/port already exists"},
		{inbounds.ErrInboundUnsupportedProtocolTransport, http.StatusBadRequest, "unsupported inbound"},
		{inbounds.ErrInboundNotFound, http.StatusNotFound, "404"},
		{errors.New("custom failure"), http.StatusInternalServerError, "custom failure"},
	}
	for _, tc := range cases {
		t.Run(tc.err.Error(), func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeInboundManagementError(rec, tc.err)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("body=%s", rec.Body.String())
			}
		})
	}
}

func TestHandleOlcrtcRoom(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev"})

	post := httptest.NewRequest(http.MethodPost, "/api/olcrtc/room", strings.NewReader(`{"provider":"jitsi"}`))
	post.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	state.handleOlcrtcRoom(rec, post)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "roomID") {
		t.Fatalf("expected roomID, got %s", rec.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/api/olcrtc/room", nil)
	rec = httptest.NewRecorder()
	state.handleOlcrtcRoom(rec, get)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d", rec.Code)
	}
}

func TestIsOlcrtcKey(t *testing.T) {
	valid := strings.Repeat("a", 64)
	if !isOlcrtcKey(valid) {
		t.Fatal("expected valid key")
	}
	if isOlcrtcKey(strings.Repeat("g", 64)) {
		t.Fatal("expected invalid hex")
	}
	if isOlcrtcKey(strings.Repeat("a", 63)) {
		t.Fatal("expected invalid length")
	}
}

func TestHandleInboundByNameValidation(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev"})

	get := httptest.NewRequest(http.MethodGet, "/api/inbounds/", nil)
	rec := httptest.NewRecorder()
	state.handleInboundByName(rec, get)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("empty name status=%d", rec.Code)
	}

	get = httptest.NewRequest(http.MethodGet, "/api/inbounds/foo/bar", nil)
	rec = httptest.NewRecorder()
	state.handleInboundByName(rec, get)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nested path status=%d", rec.Code)
	}
}
