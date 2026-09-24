package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/observability"
)

// stageRouteScope is the staged public API surface (durable apply workflow,
// v1 client/traffic API, public subscription feeds) these contracts cover.
var stageRoutePrefixes = []string{"/api/apply/", "/api/v1/", "/s/"}

func isStageRoute(path string) bool {
	if path == "/api/apply" {
		return true
	}
	for _, prefix := range stageRoutePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// newRouteInventoryMux builds the bare ServeMux with the exact production
// registrations so tests can ask mux.Handler which pattern serves a path.
// That proof is required because a handler-level "resource missing" 404 is
// indistinguishable from a mux miss on the wire (both use writeNotFound).
func newRouteInventoryMux(t *testing.T) *http.ServeMux {
	t.Helper()
	dir := t.TempDir()
	info := ServerInfo{Version: "test", Mode: "dev", StatePath: filepath.Join(dir, "state.json"), ApplyRoot: dir}
	state := newManagementState(info)
	t.Cleanup(func() {
		if err := state.Close(); err != nil {
			t.Errorf("close management state: %v", err)
		}
	})
	mux := http.NewServeMux()
	RouterComposition{}.registerMux(mux, info, state, observability.NewMetricsCollector(), "/")
	return mux
}

// instantiateRouteTemplate replaces each {param} segment with a concrete
// probe value derived from the parameter name.
func instantiateRouteTemplate(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			parts[i] = "probe-" + strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
		}
	}
	return strings.Join(parts, "/")
}

// TestRealRouterCoversOpenAPIStageEndpoints asserts that every stage route
// documented in OpenAPI is actually served by the real router with the
// documented method. The probe set is derived from the spec — all documented
// stage path+method pairs — never a hand-maintained subset (issue #923).
func TestRealRouterCoversOpenAPIStageEndpoints(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	mux := newRouteInventoryMux(t)
	documented := openAPIRouteMethods(t, "../../docs/openapi.yaml")

	var paths []string
	for path := range documented {
		if isStageRoute(path) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		t.Fatal("no stage routes found in OpenAPI document")
	}
	for _, template := range paths {
		probe := instantiateRouteTemplate(template)
		for _, docMethod := range documented[template] {
			method := strings.ToUpper(docMethod)
			// Registration proof: the mux itself must map this path to a
			// specific pattern, not the "/" catch-all. A handler-level
			// resource 404 (unknown client/token/job) cannot prove this.
			if _, pattern := mux.Handler(httptest.NewRequest(method, probe, nil)); pattern == "" || pattern == "/" {
				t.Errorf("%s %s: documented but no router registration serves it", method, template)
				continue
			}
			// Method proof: the live middleware+handler stack must not
			// reject the documented method (405 = router/spec drift) or
			// fail closed on a missing authorization policy.
			// A pre-cancelled context keeps SSE streams from blocking; the
			// assertions below only care about 405/policy-404 outcomes.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(method, probe, nil).WithContext(ctx))
			if w.Code == http.StatusMethodNotAllowed {
				t.Errorf("%s %s: method not allowed (router/spec method drift)", method, template)
			}
			if w.Code == http.StatusNotFound && strings.Contains(w.Body.String(), "endpoint authorization policy is not defined") {
				t.Errorf("%s %s: no endpoint authorization policy (route unreachable)", method, template)
			}
		}
	}
}

// muxRegistrationArgRE captures the first argument of a mux.HandleFunc /
// mux.Handle call, including string-concatenation expressions.
var muxRegistrationArgRE = regexp.MustCompile(`mux\.Handle(?:Func)?\(\s*((?:"(?:[^"\\]|\\.)*"|[A-Za-z_][A-Za-z0-9_.]*)(?:\s*\+\s*(?:"(?:[^"\\]|\\.)*"|[A-Za-z_][A-Za-z0-9_.]*))*)`)

var muxRegistrationTokenRE = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"|([A-Za-z_][A-Za-z0-9_.]*)`)

// registeredMuxPatterns scans the api package sources for mux registrations
// and returns the registered path patterns. Dynamic segments built from
// variables ("/api/protocols/"+protocol+"/room") become {} wildcards.
func registeredMuxPatterns(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read api package dir: %v", err)
	}
	seen := map[string]bool{}
	var patterns []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, arg := range muxRegistrationArgRE.FindAllSubmatch(body, -1) {
			pattern := normalizeMuxPatternArg(string(arg[1]))
			if pattern != "" && !seen[pattern] {
				seen[pattern] = true
				patterns = append(patterns, pattern)
			}
		}
	}
	sort.Strings(patterns)
	return patterns
}

// normalizeMuxPatternArg turns a Go first-argument expression into a path
// pattern: string literals are unquoted, identifiers become {} wildcards, and
// a leading method token ("GET /x") is reduced to its path.
func normalizeMuxPatternArg(arg string) string {
	var b strings.Builder
	for _, token := range muxRegistrationTokenRE.FindAllStringSubmatch(arg, -1) {
		if token[1] != "" || strings.HasPrefix(token[0], "\"") {
			b.WriteString(token[1])
		} else {
			b.WriteString("{}")
		}
	}
	pattern := b.String()
	if !strings.HasPrefix(pattern, "/") {
		if i := strings.LastIndex(pattern, " /"); i >= 0 {
			pattern = pattern[i+1:]
		}
	}
	return pattern
}

// registrationServesDocPath reports whether a registered mux pattern can
// serve the documented OpenAPI path template. Subtree registrations
// (trailing slash) serve any documented descendant; {} wildcard segments
// (dynamic registrations) and {param} template segments each match a single
// concrete segment.
func registrationServesDocPath(registration, docPath string) bool {
	subtree := strings.HasSuffix(registration, "/")
	regSegs := splitNonEmpty(registration, "/")
	docSegs := splitNonEmpty(docPath, "/")
	if subtree {
		if len(docSegs) < len(regSegs) {
			return false
		}
	} else if len(docSegs) != len(regSegs) {
		return false
	}
	for i, seg := range regSegs {
		if i >= len(docSegs) {
			return false
		}
		doc := docSegs[i]
		if seg == doc || seg == "{}" {
			continue
		}
		if strings.HasPrefix(doc, "{") && strings.HasSuffix(doc, "}") {
			continue
		}
		return false
	}
	return true
}

// TestOpenAPIDocumentsEveryRegisteredStageRoute fails when the router
// registers a stage route that OpenAPI does not document. The route set is
// scraped from the package's mux.HandleFunc registrations — registering a
// new stage route without an OpenAPI entry fails this test (issue #923).
func TestOpenAPIDocumentsEveryRegisteredStageRoute(t *testing.T) {
	documented := openAPIRouteMethods(t, "../../docs/openapi.yaml")
	var documentedPaths []string
	for path := range documented {
		documentedPaths = append(documentedPaths, path)
	}
	for _, pattern := range registeredMuxPatterns(t) {
		if !isStageRoute(pattern) {
			continue
		}
		covered := false
		for _, docPath := range documentedPaths {
			if registrationServesDocPath(pattern, docPath) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("registered route %s has no documented OpenAPI path", pattern)
		}
	}
	// Sanity: the spec must actually contain the key stage routes so an
	// emptied-out stage section cannot satisfy the check above.
	for _, must := range []string{
		"/api/apply", "/api/apply/state", "/api/apply/jobs", "/api/apply/jobs/{id}",
		"/api/apply/jobs/{id}/retry", "/api/apply/reconcile", "/api/apply/rollback",
		"/api/apply/history", "/api/apply/plan",
		"/api/v1/clients", "/api/v1/clients/{id}", "/api/v1/clients/bulk",
		"/api/v1/clients/{id}/tokens", "/api/v1/traffic/top",
		"/api/v1/traffic/{id}", "/api/v1/traffic/{id}/history",
		"/api/v1/traffic/stream", "/api/v1/events", "/s/{token}",
	} {
		if _, ok := documented[must]; !ok {
			t.Errorf("OpenAPI missing required stage route %s", must)
		}
	}
}
