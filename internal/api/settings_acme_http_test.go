package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettingsPutRejectsDNS01AcmeChallengeMode(t *testing.T) {
	r, state := newSettingsEchoRouter(t)
	body := `{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com","acmeChallengeMode":"dns-01"}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("dns-01 put: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "acmeChallengeMode must be http-01 or tls-alpn-01") {
		t.Fatalf("error body = %s", w.Body.String())
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.settings.AcmeChallengeMode == "dns-01" {
		t.Fatal("dns-01 must not be persisted")
	}
}

func TestSettingsPutRejectsUnknownAcmeChallengeMode(t *testing.T) {
	r, _ := newSettingsEchoRouter(t)
	code := putSettings(t, r, `{"panelListen":"127.0.0.1:2096","mode":"dev","acmeChallengeMode":"bogus"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("bogus mode put: %d", code)
	}
}

func TestSettingsPutAcceptsTLSALPN01AcmeChallengeMode(t *testing.T) {
	r, state := newSettingsEchoRouter(t)
	if code := putSettings(t, r, `{"panelListen":"127.0.0.1:2096","mode":"dev","acmeChallengeMode":"tls-alpn-01"}`); code != http.StatusOK {
		t.Fatalf("tls-alpn-01 put: %d", code)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.settings.AcmeChallengeMode != "tls-alpn-01" {
		t.Fatalf("acmeChallengeMode = %q", state.settings.AcmeChallengeMode)
	}
}
