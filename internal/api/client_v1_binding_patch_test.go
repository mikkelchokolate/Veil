package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestV1BindingPatchAndServerRotate covers the A7 binding endpoints:
// PATCH /bindings/{id} toggles enabled; POST /credentials/{id}/rotate with no
// value returns a server-generated one-time plaintext; the binding read model
// carries credential metadata (configured/version).
func TestV1BindingPatchAndServerRotate(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)

	inboundBody := strings.NewReader(`{"name":"hy2","protocol":"hysteria2","transport":"udp","port":18443,"enabled":true}`)
	iw := httptest.NewRecorder()
	ireq := httptest.NewRequest(http.MethodPost, "/api/inbounds", inboundBody)
	ireq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(iw, ireq)
	if iw.Code != http.StatusOK && iw.Code != http.StatusCreated {
		t.Fatalf("create inbound: %d %s", iw.Code, iw.Body.String())
	}

	id := createV1ClientWithBinding(t, r, "a7-client", "hy2", "pass-1")

	// Read binding id + version from the read model.
	get := func() map[string]any {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/clients/"+id, nil))
		var v map[string]any
		_ = json.NewDecoder(w.Body).Decode(&v)
		return v
	}
	bindings, _ := get()["bindings"].([]any)
	if len(bindings) == 0 {
		t.Fatalf("no bindings in view")
	}
	b0, _ := bindings[0].(map[string]any)
	bindingID, _ := b0["id"].(string)
	version, _ := b0["version"].(float64)
	if bindingID == "" {
		t.Fatalf("binding missing id: %v", b0)
	}
	// credential metadata present and configured
	credMeta, _ := b0["credential"].(map[string]any)
	if credMeta == nil || credMeta["configured"] != true {
		t.Fatalf("expected configured credential meta, got: %v", b0)
	}

	// PATCH binding: disable.
	patchBody := strings.NewReader(`{"enabled":false,"version":` + itoa(version) + `}`)
	pw := httptest.NewRecorder()
	preq := httptest.NewRequest(http.MethodPatch, "/api/v1/clients/"+id+"/bindings/"+bindingID, patchBody)
	preq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(pw, preq)
	if pw.Code != http.StatusOK {
		t.Fatalf("patch binding: %d %s", pw.Code, pw.Body.String())
	}
	var patched map[string]any
	_ = json.NewDecoder(pw.Body).Decode(&patched)
	if en, _ := patched["enabled"].(bool); en != false {
		t.Fatalf("binding not disabled: %v", patched)
	}

	// Server-generated rotate.
	rw := httptest.NewRecorder()
	rreq := httptest.NewRequest(http.MethodPost, "/api/v1/clients/"+id+"/credentials/"+bindingID+"/rotate", strings.NewReader(`{}`))
	rreq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rw, rreq)
	if rw.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", rw.Code, rw.Body.String())
	}
	var rot map[string]any
	_ = json.NewDecoder(rw.Body).Decode(&rot)
	pt, _ := rot["plaintext"].(string)
	if pt == "" {
		t.Fatalf("server rotate returned no one-time plaintext: %v", rot)
	}
	if len(pt) < 32 {
		t.Fatalf("generated plaintext too weak (len=%d)", len(pt))
	}
}

func itoa(f float64) string {
	return strings.TrimRight(strings.TrimRight(jsonNumber(f), "0"), ".")
}

func jsonNumber(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}
