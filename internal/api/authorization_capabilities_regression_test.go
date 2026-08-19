package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type routeAuthorizationExpectation struct {
	method      string
	path        string
	public      bool
	viewer      bool
	description string
}

// productionAuthorizationMatrix is deliberately exhaustive against the
// generated OpenAPI route contract. Every operation has an explicit access
// decision: unknown operations must be denied rather than inheriting access
// from an HTTP method.
func productionAuthorizationMatrix() []routeAuthorizationExpectation {
	return []routeAuthorizationExpectation{
		{http.MethodGet, "/healthz", true, true, "health"},
		{http.MethodGet, "/metrics", true, true, "metrics when not configured private"},
		{http.MethodGet, "/api/setup/status", true, true, "setup bootstrap"},
		{http.MethodPost, "/api/setup/complete", true, true, "setup bootstrap"},
		{http.MethodPost, "/api/auth/login", true, true, "login"},
		{http.MethodPost, "/api/auth/logout", true, true, "logout"},
		{http.MethodGet, "/api/auth/status", true, true, "auth status"},
		{http.MethodPost, "/api/auth/locale", false, true, "self service"},
		{http.MethodGet, "/api/auth/sessions", false, false, "admin metadata"},
		{http.MethodDelete, "/api/auth/sessions", false, false, "admin mutation"},
		{http.MethodPost, "/api/admin/rotate-key", false, false, "admin mutation"},
		{http.MethodGet, "/api/audit", false, false, "admin metadata"},
		{http.MethodGet, "/api/backups", false, false, "admin metadata"},
		{http.MethodPost, "/api/backups", false, false, "admin mutation"},
		{http.MethodPost, "/api/backups/prune", false, false, "admin mutation"},
		{http.MethodGet, "/api/backups/example.tar.age/download", false, false, "admin secret material"},
		{http.MethodPost, "/api/backups/example.tar.age/verify", false, false, "admin mutation"},
		{http.MethodPost, "/api/backups/example.tar.age/restore", false, false, "admin mutation"},
		{http.MethodDelete, "/api/backups/example.tar.age", false, false, "admin mutation"},
		{http.MethodGet, "/api/backup-restore-jobs/job-1", false, true, "self-service recovery capability"},
		{http.MethodGet, "/api/users", false, false, "admin metadata"},
		{http.MethodPost, "/api/users", false, false, "admin mutation"},
		{http.MethodDelete, "/api/users/alice", false, false, "admin mutation"},
		{http.MethodPut, "/api/users/alice", false, false, "admin mutation"},
		{http.MethodGet, "/api/status", false, true, "viewer metadata"},
		{http.MethodGet, "/api/version", false, true, "viewer metadata"},
		{http.MethodPost, "/api/version/update", false, false, "admin mutation"},
		{http.MethodGet, "/api/settings", false, true, "redacted viewer metadata"},
		{http.MethodPut, "/api/settings", false, false, "admin mutation"},
		{http.MethodGet, "/api/protocols", false, true, "viewer metadata"},
		{http.MethodGet, "/api/inbounds", false, true, "redacted viewer metadata"},
		{http.MethodPost, "/api/inbounds", false, false, "admin mutation"},
		{http.MethodGet, "/api/inbounds/edge", false, true, "redacted viewer metadata"},
		{http.MethodPut, "/api/inbounds/edge", false, false, "admin mutation"},
		{http.MethodDelete, "/api/inbounds/edge", false, false, "admin mutation"},
		{http.MethodGet, "/api/routing/rules", false, true, "viewer metadata"},
		{http.MethodPost, "/api/routing/rules", false, false, "admin mutation"},
		{http.MethodGet, "/api/routing/rules/direct", false, true, "viewer metadata"},
		{http.MethodPut, "/api/routing/rules/direct", false, false, "admin mutation"},
		{http.MethodDelete, "/api/routing/rules/direct", false, false, "admin mutation"},
		{http.MethodGet, "/api/routing/presets", false, true, "viewer metadata"},
		{http.MethodPost, "/api/routing/presets/ru-recommended", false, false, "admin mutation"},
		{http.MethodGet, "/api/warp", false, true, "redacted viewer metadata"},
		{http.MethodPut, "/api/warp", false, false, "admin mutation"},
		{http.MethodGet, "/api/client-links", false, false, "admin secret material"},
		{http.MethodGet, "/api/client-links/subscription", false, false, "admin secret material"},
		{http.MethodPost, "/api/client-links/qr", false, false, "admin secret material"},
		{http.MethodGet, "/api/firewall", false, true, "viewer metadata"},
		{http.MethodPost, "/api/apply", false, false, "admin mutation"},
		{http.MethodGet, "/api/apply/state", false, true, "viewer metadata"},
		{http.MethodGet, "/api/apply/jobs", false, true, "viewer metadata"},
		{http.MethodGet, "/api/apply/jobs/job-1", false, true, "viewer metadata"},
		{http.MethodPost, "/api/apply/jobs/job-1/retry", false, false, "admin mutation"},
		{http.MethodPost, "/api/apply/reconcile", false, false, "admin mutation"},
		{http.MethodPost, "/api/validation", false, false, "admin metadata"},
		{http.MethodPost, "/api/apply/plan", false, true, "viewer diagnostic"},
		{http.MethodGet, "/api/apply/history", false, true, "viewer metadata"},
		{http.MethodPost, "/api/profiles/ru-recommended/preview", false, true, "viewer diagnostic"},
		{http.MethodPost, "/api/services/veil-caddy/restart", false, false, "admin mutation"},
		{http.MethodGet, "/api/system", false, true, "viewer metadata"},
		{http.MethodGet, "/api/tls", false, true, "viewer metadata"},
		{http.MethodGet, "/api/network", false, true, "viewer metadata"},
		{http.MethodGet, "/api/connections", false, true, "viewer metadata"},
		{http.MethodGet, "/api/processes", false, true, "viewer metadata"},
		{http.MethodGet, "/api/disk", false, true, "viewer metadata"},
		{http.MethodGet, "/api/runtime/observation", false, true, "viewer metadata"},
		{http.MethodGet, "/api/logs", false, false, "admin secret material"},
		{http.MethodPost, "/api/tools/dns-lookup", false, true, "viewer diagnostic"},
		{http.MethodPost, "/api/tools/ping", false, true, "viewer diagnostic"},
		{http.MethodPost, "/api/tools/speedtest", false, true, "viewer diagnostic"},
		{http.MethodGet, "/api/v1/clients", false, true, "viewer metadata"},
		{http.MethodPost, "/api/v1/clients", false, false, "admin mutation"},
		{http.MethodPost, "/api/v1/clients/bulk", false, false, "admin mutation"},
		{http.MethodPost, "/api/v1/clients/migrate-legacy", false, false, "admin mutation"},
		{http.MethodGet, "/api/v1/clients/client-1", false, true, "viewer metadata"},
		{http.MethodPatch, "/api/v1/clients/client-1", false, false, "admin mutation"},
		{http.MethodDelete, "/api/v1/clients/client-1", false, false, "admin mutation"},
		{http.MethodGet, "/api/v1/clients/client-1/bindings", false, true, "viewer metadata"},
		{http.MethodPost, "/api/v1/clients/client-1/bindings", false, false, "admin mutation"},
		{http.MethodPatch, "/api/v1/clients/client-1/bindings/binding-1", false, false, "admin mutation"},
		{http.MethodDelete, "/api/v1/clients/client-1/bindings/binding-1", false, false, "admin mutation"},
		{http.MethodPost, "/api/v1/clients/client-1/credentials/binding-1", false, false, "admin secret write"},
		{http.MethodPost, "/api/v1/clients/client-1/credentials/binding-1/rotate", false, false, "admin secret read/write"},
		{http.MethodGet, "/api/v1/clients/client-1/tokens", false, false, "admin metadata"},
		{http.MethodPost, "/api/v1/clients/client-1/tokens", false, false, "admin secret read/write"},
		{http.MethodGet, "/api/v1/clients/client-1/tokens/token-1", false, false, "admin secret material"},
		{http.MethodDelete, "/api/v1/clients/client-1/tokens/token-1", false, false, "admin mutation"},
		{http.MethodGet, "/api/v1/clients/client-1/links", false, false, "admin secret material"},
		{http.MethodPost, "/api/v1/clients/client-1/tokens/token-1/rotate", false, false, "admin secret read/write"},
		{http.MethodGet, "/api/v1/traffic/summary", false, true, "viewer metadata"},
		{http.MethodGet, "/api/v1/traffic/top", false, true, "viewer metadata"},
		{http.MethodGet, "/api/v1/traffic/client-1", false, true, "viewer metadata"},
		{http.MethodGet, "/api/v1/traffic/client-1/history", false, true, "viewer metadata"},
		{http.MethodGet, "/api/v1/traffic/stream", false, true, "viewer stream"},
		{http.MethodGet, "/api/v1/events", false, true, "viewer stream"},
		{http.MethodGet, "/s/public-token", true, true, "public token capability"},
		{http.MethodHead, "/s/public-token", true, true, "public token capability"},
	}
}

func TestEveryOperationHasExplicitAnonymousViewerAdminAuthorization(t *testing.T) {
	state := &managementState{
		users: []User{
			{Username: "viewer", Role: "viewer"},
			{Username: "admin", Role: "admin"},
		},
		sessions: mustNewSessionRegistry(""),
	}
	viewer := mustCreateSession(t, state.sessions, "viewer", "viewer")
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := authMiddlewareWithOptions(state, authMiddlewareOptions{
		Token:             "admin-token",
		AllowDevAnonymous: false,
		AllowSetup:        true,
	}, next)

	for _, tc := range productionAuthorizationMatrix() {
		name := tc.method + " " + tc.path
		t.Run(name+"/anonymous", func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			want := http.StatusUnauthorized
			if tc.public {
				want = http.StatusNoContent
			}
			if rec.Code != want {
				t.Fatalf("anonymous status=%d want=%d (%s)", rec.Code, want, tc.description)
			}
		})
		t.Run(name+"/viewer", func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.AddCookie(&http.Cookie{Name: "veil_session", Value: viewer.Token})
			req.Header.Set("X-CSRF-Token", viewer.CSRFToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			want := http.StatusForbidden
			if tc.public || tc.viewer {
				want = http.StatusNoContent
			}
			if rec.Code != want {
				t.Fatalf("viewer status=%d want=%d (%s)", rec.Code, want, tc.description)
			}
		})
		t.Run(name+"/admin", func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("X-Veil-Token", "admin-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("admin status=%d want=%d (%s)", rec.Code, http.StatusNoContent, tc.description)
			}
		})
	}
}

func TestViewerResponsesNeverContainSecretMaterial(t *testing.T) {
	state := &managementState{
		settings: Settings{
			PanelListen:       "127.0.0.1:2096",
			Mode:              "dev",
			NaiveUsername:     "secret-user",
			NaivePassword:     "naive-secret-value",
			Hysteria2Password: "hy2-secret-value",
			OlcrtcAuth:        "olcrtc-secret-value",
			ProtocolFields: map[string]any{
				"customPassword": "protocol-secret-value",
				"publicOption":   "visible-value",
			},
		},
		warp: WarpConfig{
			Enabled:       true,
			LicenseKey:    "warp-license-secret",
			PrivateKey:    "warp-private-secret",
			Endpoint:      "engage.cloudflareclient.com:2408",
			PeerPublicKey: "public-peer-value",
		},
		inbounds: []Inbound{{
			Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true,
			Password: "link-password-secret",
		}},
		users:    []User{{Username: "viewer", Role: "viewer"}},
		sessions: mustNewSessionRegistry(""),
	}
	viewer := mustCreateSession(t, state.sessions, "viewer", "viewer")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/settings", state.handleSettings)
	mux.HandleFunc("/api/warp", state.handleWarp)
	mux.HandleFunc("/api/client-links", state.handleClientLinks)
	mux.HandleFunc("/api/client-links/subscription", state.handleClientLinksSubscription)
	LogRoutes{State: state}.Register(mux)
	handler := authMiddlewareWithOptions(state, authMiddlewareOptions{AllowDevAnonymous: false}, mux)

	redacted := []struct {
		path       string
		secretKeys []string
		secrets    []string
	}{
		{
			path:       "/api/settings",
			secretKeys: []string{"naiveUsername", "naivePassword", "hysteria2Password", "olcrtcAuth", "customPassword"},
			secrets:    []string{"secret-user", "naive-secret-value", "hy2-secret-value", "olcrtc-secret-value", "protocol-secret-value"},
		},
		{
			path:       "/api/warp",
			secretKeys: []string{"licenseKey", "privateKey"},
			secrets:    []string{"warp-license-secret", "warp-private-secret"},
		},
	}
	for _, tc := range redacted {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.AddCookie(&http.Cookie{Name: "veil_session", Value: viewer.Token})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var body any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			encoded := rec.Body.String()
			for _, secret := range tc.secrets {
				if strings.Contains(encoded, secret) {
					t.Errorf("viewer response leaked secret value %q: %s", secret, encoded)
				}
			}
			for _, key := range tc.secretKeys {
				if jsonContainsKey(body, key) {
					t.Errorf("viewer response exposed secret field %q: %s", key, encoded)
				}
			}
		})
	}

	for _, path := range []string{"/api/client-links", "/api/client-links/subscription", "/api/logs"} {
		t.Run(path+"-admin-only", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.AddCookie(&http.Cookie{Name: "veil_session", Value: viewer.Token})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("viewer status=%d want=403 body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func jsonContainsKey(value any, key string) bool {
	switch value := value.(type) {
	case map[string]any:
		for k, child := range value {
			if k == key || jsonContainsKey(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if jsonContainsKey(child, key) {
				return true
			}
		}
	}
	return false
}

func TestEveryOpenAPIOperationDeclaresRequiredRole(t *testing.T) {
	file, err := os.Open("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	allowed := map[string]bool{
		"public": true, "viewer": true, "admin-metadata": true,
		"admin-secret": true, "admin-mutation": true, "self-service": true,
	}
	scanner := bufio.NewScanner(file)
	currentPath := ""
	currentMethod := ""
	currentRole := ""
	flush := func() {
		if currentMethod == "" {
			return
		}
		if !allowed[currentRole] {
			t.Errorf("%s %s has no valid x-veil-role (got %q)", strings.ToUpper(currentMethod), currentPath, currentRole)
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "  /") && strings.HasSuffix(line, ":") {
			flush()
			currentMethod, currentRole = "", ""
			currentPath = strings.TrimSuffix(strings.TrimSpace(line), ":")
			continue
		}
		if currentPath == "" {
			continue
		}
		if len(line) >= 5 && line[:4] == "    " && line[4] != ' ' {
			candidate := strings.TrimSuffix(strings.TrimSpace(line), ":")
			switch candidate {
			case "get", "post", "put", "patch", "delete", "head", "options", "trace":
				flush()
				currentMethod, currentRole = candidate, ""
			}
			continue
		}
		if currentMethod != "" && strings.HasPrefix(strings.TrimSpace(line), "x-veil-role:") {
			currentRole = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "x-veil-role:"))
			currentRole = strings.Trim(currentRole, `"'`)
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}
