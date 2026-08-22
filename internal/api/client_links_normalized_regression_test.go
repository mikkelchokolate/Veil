package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestLegacyClientLinkSubscriptionUsesNormalizedRuntimeCredentials(t *testing.T) {
	state, service, _ := newRuntimeIdentityTestState(t)
	created, err := service.Create(client.Client{Name: "normalized-client", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := service.AddBinding(created.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetCredential(binding.ID, "password", "normalized-secret"); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/client-links/subscription?format=raw", nil)
	state.handleClientLinksSubscription(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("subscription status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, binding.RuntimeIdentity) || !strings.Contains(body, "normalized-secret") {
		t.Fatalf("subscription omitted normalized runtime credential: %q", body)
	}
	if strings.Contains(body, "inbound-pass") {
		t.Fatalf("subscription exported inactive legacy credential: %q", body)
	}
}
