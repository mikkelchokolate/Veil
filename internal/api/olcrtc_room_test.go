package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleOlcrtcRoomGeneratesForJitsi(t *testing.T) {
	state := &managementState{}
	w := httptest.NewRecorder()
	state.handleOlcrtcRoom(w, httptest.NewRequest(http.MethodPost, "/api/olcrtc/room", strings.NewReader(`{"provider":"jitsi"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("jitsi room generation expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Provider string `json:"provider"`
		RoomID   string `json:"roomID"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.RoomID == "" || !strings.HasPrefix(resp.RoomID, "https://") {
		t.Fatalf("expected a jitsi room URL, got %q", resp.RoomID)
	}
}

func TestHandleOlcrtcRoomRefusesManualProvider(t *testing.T) {
	state := &managementState{}
	w := httptest.NewRecorder()
	state.handleOlcrtcRoom(w, httptest.NewRequest(http.MethodPost, "/api/olcrtc/room", strings.NewReader(`{"provider":"telemost"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("telemost room generation should be 400 (manual room), got %d: %s", w.Code, w.Body.String())
	}
}
