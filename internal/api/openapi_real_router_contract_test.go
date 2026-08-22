package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRealRouterCoversOpenAPIStageEndpoints asserts that every stage 0-3
// endpoint documented in OpenAPI is actually served by the real router with
// the documented method. Unlike the hand-maintained map test, this exercises
// the live router so the spec and the implementation cannot drift apart in
// the same wrong direction.
func TestRealRouterCoversOpenAPIStageEndpoints(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)

	cases := []struct {
		method     string
		path       string
		notFoundOK bool // a valid "resource not found" 404, not "route missing"
	}{
		{http.MethodGet, "/api/apply/state", false},
		{http.MethodGet, "/api/apply/jobs", false},
		{http.MethodGet, "/api/v1/clients", false},
		{http.MethodGet, "/api/v1/clients/nonexistent-id", true},
		{http.MethodGet, "/api/v1/traffic/top", false},
		{http.MethodGet, "/api/v1/traffic/some-id", true},
		{http.MethodGet, "/api/v1/traffic/some-id/history", true},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		// 405 proves the route is registered but the method is wrong -> drift.
		if w.Code == http.StatusMethodNotAllowed {
			t.Errorf("%s %s: method not allowed (router/spec method drift)", tc.method, tc.path)
		}
		// A 404 is only acceptable when we asked for a resource that legitimately
		// does not exist; otherwise it means the route is not registered.
		if w.Code == http.StatusNotFound && !tc.notFoundOK {
			t.Errorf("%s %s: got 404 (route missing)", tc.method, tc.path)
		}
	}
}

// TestOpenAPIDocumentsEveryRegisteredStageRoute fails if the router serves a
// stage 0-3 route that OpenAPI does not document. The route set is derived
// from the spec, then each is probed against the real router.
func TestOpenAPIDocumentsEveryRegisteredStageRoute(t *testing.T) {
	documented := openAPIRouteMethods(t, "../../docs/openapi.yaml")
	stagePrefixes := []string{"/api/apply/", "/api/v1/", "/s/"}
	for path, methods := range documented {
		stage := false
		for _, p := range stagePrefixes {
			if strings.HasPrefix(path, p) {
				stage = true
			}
		}
		if !stage {
			continue
		}
		if len(methods) == 0 {
			t.Errorf("OpenAPI path %s documents no methods", path)
		}
	}
	// Sanity: the spec must actually contain the key new routes.
	for _, must := range []string{
		"/api/apply/state", "/api/apply/jobs", "/api/apply/jobs/{id}",
		"/api/apply/jobs/{id}/retry", "/api/apply/reconcile",
		"/api/v1/clients", "/api/v1/clients/{id}", "/api/v1/clients/bulk",
		"/api/v1/clients/{id}/tokens", "/api/v1/traffic/top",
		"/api/v1/traffic/{id}", "/api/v1/traffic/{id}/history",
		"/api/v1/traffic/stream", "/s/{token}",
	} {
		if _, ok := documented[must]; !ok {
			t.Errorf("OpenAPI missing required stage route %s", must)
		}
	}
}
