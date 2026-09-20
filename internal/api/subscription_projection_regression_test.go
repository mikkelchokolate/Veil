package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestAppliedClientProjectionDoesNotAliasCache(t *testing.T) {
	cached := managementSnapshot{}
	cached.Settings.ProtocolFields = map[string]any{"password": "original"}
	cached.Inbounds = []model.Inbound{{
		Name:           "hy2",
		Protocol:       "hysteria2",
		ProtocolFields: map[string]any{"hysteria2Password": "original-inbound"},
		Profiles:       []model.ClientProfile{{Name: "owner", Password: "profile-secret"}},
	}}
	s := &managementState{
		appliedProjectionRevision: 1,
		appliedProjections:        map[string]managementSnapshot{"client-1": cached},
	}

	first, err := s.appliedClientProjection(1, nil, "client-1")
	if err != nil {
		t.Fatal(err)
	}
	first.Settings.ProtocolFields["password"] = "changed by request"
	first.Inbounds[0].ProtocolFields["hysteria2Password"] = "changed-inbound"
	first.Inbounds[0].Profiles[0].Password = "changed-profile"
	first.Inbounds = append(first.Inbounds, model.Inbound{Name: "extra"})

	second, err := s.appliedClientProjection(1, nil, "client-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := second.Settings.ProtocolFields["password"]; got != "original" {
		t.Fatalf("cached projection mutated by another request: %v", got)
	}
	if got := second.Inbounds[0].ProtocolFields["hysteria2Password"]; got != "original-inbound" {
		t.Fatalf("cached inbound protocol fields mutated by another request: %v", got)
	}
	if got := second.Inbounds[0].Profiles[0].Password; got != "profile-secret" {
		t.Fatalf("cached inbound profiles mutated by another request: %v", got)
	}
	if len(second.Inbounds) != 1 {
		t.Fatalf("cached inbound slice mutated by another request: %+v", second.Inbounds)
	}
}

func TestConcurrentPublicSubscriptionsDoNotRaceCachedProjection(t *testing.T) {
	router, _ := newSubscriptionTestRouter(t)
	firstToken, _ := seedClientWithToken(t, router)
	second := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"bob","bindings":[{"inboundId":"hy2-sub","credential":"pw-bob"}]}`)
	if second.Code != http.StatusCreated {
		t.Fatalf("seed second client: %d %s", second.Code, second.Body.String())
	}
	created := unwrapClient(t, second.Body.Bytes())
	secondID, _ := created["id"].(string)
	issued := v1Request(t, router, http.MethodPost, "/api/v1/clients/"+secondID+"/tokens", `{"label":"phone"}`)
	if issued.Code != http.StatusCreated {
		t.Fatalf("seed second token: %d %s", issued.Code, issued.Body.String())
	}
	var tok struct {
		Plaintext string `json:"plaintext"`
	}
	if err := json.Unmarshal(issued.Body.Bytes(), &tok); err != nil || tok.Plaintext == "" {
		t.Fatalf("second plaintext: %v body=%s", err, issued.Body.String())
	}

	settings := v1Request(t, router, http.MethodPut, "/api/settings",
		`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com","hysteria2Password":"shared-secret"}`)
	if settings.Code != http.StatusOK {
		t.Fatalf("set hysteria2 password: %d %s", settings.Code, settings.Body.String())
	}

	tokens := []string{firstToken, tok.Plaintext, firstToken}
	var wg sync.WaitGroup
	errCh := make(chan error, 48)
	seq := 0
	for i := 0; i < 16; i++ {
		for _, token := range tokens {
			seq++
			wg.Add(1)
			go func(token string, source int) {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodGet, "/s/"+token+"?format=raw", nil)
				// Each request carries a distinct source IP so the shared /s/
				// read-path budget (30/min + burst 6, audit #337) is not what
				// this race test exercises — the per-token/source subscription
				// limiter (60/300 per minute) stays far above the fan-out too.
				req.RemoteAddr = fmt.Sprintf("198.51.100.%d:40000", source)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					errCh <- fmt.Errorf("subscription status=%d body=%s", w.Code, w.Body.String())
				}
			}(token, seq)
		}
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}
