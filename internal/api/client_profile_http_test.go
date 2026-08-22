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

func TestInboundCreateThroughHTTPGeneratesClientProfilePassword(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := writeRenderableManagementState(statePath, "dual"); err != nil {
		t.Fatalf("write state: %v", err)
	}
	r, _ := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath})

	body := strings.NewReader(`{"name":"naive-profiles","protocol":"naiveproxy","transport":"tcp","port":9443,"enabled":true,"profiles":[{"name":"alice","username":"alice","enabled":true}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/inbounds", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var inbound Inbound
	if err := json.NewDecoder(w.Body).Decode(&inbound); err != nil {
		t.Fatalf("decode inbound: %v", err)
	}
	if len(inbound.Profiles) != 1 {
		t.Fatalf("expected one profile: %+v", inbound)
	}
	// The profile credential is generated server-side and must be redacted in
	// the API response (viewer role and network readers must not see it).
	if inbound.Profiles[0].Password != veilsettings.RedactedSecret {
		t.Fatalf("expected redacted client profile password in response, got %q", inbound.Profiles[0].Password)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/client-links", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	links := w2.Body.String()
	// The live credential must reach client links (never the sentinel), and
	// the URI must embed a real generated password for the profile.
	if strings.Contains(links, veilsettings.RedactedSecret) {
		t.Fatalf("client links leak the redaction sentinel: %s", links)
	}
	if !strings.Contains(links, "https://alice:") || !strings.Contains(links, "@vpn.example.com:9443") {
		t.Fatalf("client links do not include the generated profile credential: %s", links)
	}
}
