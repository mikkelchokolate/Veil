package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouterCompositionTreatsExplicitRootAsRoot(t *testing.T) {
	handler, _ := NewRouterComposition(ServerInfo{
		Version:     "test",
		Mode:        "dev",
		WebBasePath: "/",
	}).Build()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("explicit root status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"//api/`) || strings.Contains(rec.Body.String(), `'//api/`) {
		t.Fatal("explicit root rendered double-slash API paths")
	}
}

func TestRouterCompositionFailsClosedForUnsafeDirectBasePath(t *testing.T) {
	unsafePath := "panel'</script>"
	handler, _ := NewRouterComposition(ServerInfo{
		Version:     "test",
		Mode:        "dev",
		WebBasePath: unsafePath,
	}).Build()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unsafe direct base path should fail closed to root: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), unsafePath) {
		t.Fatal("unsafe direct base path was inserted into rendered Panel")
	}
}

func TestRouterCompositionUsesCanonicalNestedBasePath(t *testing.T) {
	handler, _ := NewRouterComposition(ServerInfo{
		Version:     "test",
		Mode:        "dev",
		WebBasePath: "panel/admin",
	}).Build()

	allowed := httptest.NewRecorder()
	handler.ServeHTTP(allowed, httptest.NewRequest(http.MethodGet, "/panel/admin/", nil))
	if allowed.Code != http.StatusOK {
		t.Fatalf("canonical nested path status=%d body=%s", allowed.Code, allowed.Body.String())
	}

	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, httptest.NewRequest(http.MethodGet, "/panel/administrator/", nil))
	if rejected.Code != http.StatusNotFound {
		t.Fatalf("sibling prefix status=%d body=%s", rejected.Code, rejected.Body.String())
	}
}
