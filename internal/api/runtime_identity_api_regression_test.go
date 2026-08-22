package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDuplicateExplicitRuntimeIdentityOnOneInboundIsRejectedTransactionally(t *testing.T) {
	router, _ := newApplyTrackedRouter(t)
	inbound := httptest.NewRequest(http.MethodPost, "/api/inbounds", strings.NewReader(`{"name":"hy2","protocol":"hysteria2","transport":"udp","port":18443,"enabled":true}`))
	inbound.Header.Set("Content-Type", "application/json")
	inboundResponse := httptest.NewRecorder()
	router.ServeHTTP(inboundResponse, inbound)
	if inboundResponse.Code != http.StatusOK && inboundResponse.Code != http.StatusCreated {
		t.Fatalf("create inbound: status=%d body=%s", inboundResponse.Code, inboundResponse.Body.String())
	}
	firstID := createV1Client(t, router, "first")
	secondID := createV1Client(t, router, "second")
	body := `{"inboundId":"hy2","runtimeIdentity":"shared-runtime-user"}`
	first := v1Request(t, router, http.MethodPost, "/api/v1/clients/"+firstID+"/bindings", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first binding: status=%d body=%s", first.Code, first.Body.String())
	}
	second := v1Request(t, router, http.MethodPost, "/api/v1/clients/"+secondID+"/bindings", body)
	if second.Code != http.StatusConflict {
		t.Fatalf("duplicate runtime identity accepted: status=%d body=%s", second.Code, strings.TrimSpace(second.Body.String()))
	}
}
