package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/livevalidation"
	"github.com/mikkelchokolate/Veil/internal/model"
)

type stubConfigurationValidator struct {
	mu       sync.Mutex
	response livevalidation.Response
	requests []livevalidation.Request
}

func (v *stubConfigurationValidator) Validate(_ context.Context, request livevalidation.Request) livevalidation.Response {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.requests = append(v.requests, request)
	return v.response
}

type busyPortProbe struct{}

func (busyPortProbe) Available(context.Context, string, int) (bool, error) {
	return false, nil
}

func TestValidationEndpointRequiresAuthentication(t *testing.T) {
	validator := validStubValidator()
	router, _ := newTestRouter(ServerInfo{
		Version:                "test",
		Mode:                   "server",
		AuthToken:              "secret-token",
		PublicListen:           true,
		ConfigurationValidator: validator,
	})

	request := httptest.NewRequest(http.MethodPost, "/api/validation", strings.NewReader(`{"settings":{},"inbounds":[],"warp":{}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/validation", strings.NewReader(`{"settings":{},"inbounds":[],"warp":{}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Veil-Token", "secret-token")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200: %s", response.Code, response.Body.String())
	}
}

func TestValidationEndpointRejectsViewerRole(t *testing.T) {
	validator := validStubValidator()
	router, reloader := newTestRouter(ServerInfo{
		Version:                "test",
		Mode:                   "server",
		PublicListen:           true,
		ConfigurationValidator: validator,
	})
	state := reloader.(*managementState)
	state.mu.Lock()
	state.users = []User{{Username: "reader", Role: "viewer"}}
	state.mu.Unlock()
	session := mustCreateSession(t, state.sessionRegistry(), "reader", "viewer")

	request := httptest.NewRequest(http.MethodPost, "/api/validation", strings.NewReader(`{"settings":{},"inbounds":[],"warp":{}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", session.CSRFToken)
	request.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", response.Code, response.Body.String())
	}
}

func TestValidationEndpointRejectsMalformedPayload(t *testing.T) {
	router, _ := newTestRouter(ServerInfo{
		Version:                "test",
		Mode:                   "dev",
		ConfigurationValidator: validStubValidator(),
	})
	request := httptest.NewRequest(http.MethodPost, "/api/validation", strings.NewReader(`{"settings":`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
	}
}

func TestValidationEndpointReturnsStructuredIssues(t *testing.T) {
	validator := &stubConfigurationValidator{response: invalidValidationResponse()}
	router, _ := newTestRouter(ServerInfo{
		Version:                "test",
		Mode:                   "dev",
		ConfigurationValidator: validator,
	})
	request := httptest.NewRequest(http.MethodPost, "/api/validation", strings.NewReader(`{"settings":{"panelListen":"127.0.0.1:2096","mode":"dev"},"inbounds":[{"name":"edge","protocol":"mieru","transport":"tcp","port":443,"enabled":true,"password":"secret"}],"warp":{}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var payload livevalidation.Response
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Valid || len(payload.Issues) != 1 || payload.Issues[0].Code != "port_in_use" {
		t.Fatalf("unexpected response: %+v", payload)
	}
}

func TestInvalidLiveValidationDoesNotCreateInbound(t *testing.T) {
	state := newManagementState(ServerInfo{
		Mode:                   "dev",
		PanelListen:            "127.0.0.1:2096",
		ConfigurationValidator: &stubConfigurationValidator{response: invalidValidationResponse()},
	})
	mux := http.NewServeMux()
	state.register(mux)

	request := httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"edge","protocol":"mieru","transport":"tcp","port":443,"enabled":true,"password":"secret"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", response.Code, response.Body.String())
	}
	if len(state.inbounds) != 0 {
		t.Fatalf("invalid inbound was persisted: %+v", state.inbounds)
	}
	assertValidationFailure(t, response, "port_in_use")
}

func TestInvalidLiveValidationDoesNotUpdateSettings(t *testing.T) {
	state := newManagementState(ServerInfo{
		Mode:                   "dev",
		PanelListen:            "127.0.0.1:2096",
		ConfigurationValidator: &stubConfigurationValidator{response: invalidValidationResponse()},
	})
	mux := http.NewServeMux()
	state.register(mux)

	request := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"panelListen":"127.0.0.1:3096","mode":"dev"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", response.Code, response.Body.String())
	}
	if state.settings.PanelListen != "127.0.0.1:2096" {
		t.Fatalf("invalid settings were persisted: %+v", state.settings)
	}
}

func TestInvalidLiveValidationStopsApplyBeforeStaging(t *testing.T) {
	applyRoot := t.TempDir()
	state := newManagementState(ServerInfo{
		Mode:                   "dev",
		PanelListen:            "127.0.0.1:2096",
		ApplyRoot:              applyRoot,
		ConfigurationValidator: &stubConfigurationValidator{response: invalidValidationResponse()},
	})
	mux := http.NewServeMux()
	state.register(mux)

	request := httptest.NewRequest(http.MethodPost, "/api/apply", strings.NewReader(`{"confirm":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), `"applied":true`) {
		t.Fatalf("invalid apply reported success: %s", response.Body.String())
	}
}

func TestLiveValidationAllowsUnchangedPersistedBinding(t *testing.T) {
	stubManagementApplySideEffects(t)
	state := newManagementState(ServerInfo{
		Mode:        "dev",
		PanelListen: "127.0.0.1:2096",
		ConfigurationValidator: livevalidation.Validator{
			Ports: busyPortProbe{},
			Now: func() time.Time {
				return time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
			},
		},
	})
	t.Cleanup(func() {
		if err := closeClientSubsystem(state); err != nil {
			t.Errorf("close live-validation state: %v", err)
		}
	})
	state.inbounds = []Inbound{{
		Name: "edge", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "secret",
	}}
	mux := http.NewServeMux()
	state.register(mux)

	request := httptest.NewRequest(http.MethodPut, "/api/inbounds/edge", strings.NewReader(`{"protocol":"mieru","transport":"tcp","port":443,"enabled":true,"password":"[REDACTED]"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
}

func validStubValidator() *stubConfigurationValidator {
	return &stubConfigurationValidator{response: livevalidation.Response{
		Valid:     true,
		Issues:    []model.ValidationIssue{},
		CheckedAt: time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC),
	}}
}

func invalidValidationResponse() livevalidation.Response {
	return livevalidation.Response{
		Valid: false,
		Issues: []model.ValidationIssue{{
			Code:        "port_in_use",
			Severity:    "error",
			Field:       "port",
			InboundID:   "edge",
			Message:     "TCP port 443 is already in use",
			Remediation: "Choose another port.",
			Source:      "live-host",
		}},
		CheckedAt: time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC),
	}
}

func assertValidationFailure(t *testing.T, response *httptest.ResponseRecorder, code string) {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		Issues []model.ValidationIssue `json:"issues"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode validation failure: %v; body=%s", err, response.Body.String())
	}
	if payload.Error.Code != "validation_failed" {
		t.Fatalf("error code = %q", payload.Error.Code)
	}
	if len(payload.Issues) == 0 || payload.Issues[0].Code != code {
		t.Fatalf("issues = %+v, want %s", payload.Issues, code)
	}
}
