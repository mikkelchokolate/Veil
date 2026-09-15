package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	veilapply "github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/model"
)

func publicRawSubscription(t *testing.T, router http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/s/"+token+"?format=raw", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func TestPublicSubscriptionUsesLastAppliedImmutableSnapshot(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*testing.T, *managementState, string)
	}{
		{name: "credential", mutate: func(t *testing.T, state *managementState, bindingID string) {
			if _, err := state.clientCreds.Rotate(bindingID, "password", "desired-not-applied-credential"); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "port", mutate: func(_ *testing.T, state *managementState, _ string) {
			state.inbounds[0].Port++
		}},
		{name: "domain", mutate: func(_ *testing.T, state *managementState, _ string) {
			state.settings.Domain = "desired-not-applied.example.net"
		}},
		{name: "protocol", mutate: func(_ *testing.T, state *managementState, _ string) {
			state.inbounds[0].Protocol = "mieru"
			state.inbounds[0].Transport = "tcp"
		}},
		{name: "inbound_enabled", mutate: func(_ *testing.T, state *managementState, _ string) {
			state.inbounds[0].Enabled = false
		}},
		{name: "warp_routing", mutate: func(_ *testing.T, state *managementState, _ string) {
			state.warp = model.WarpConfig{Enabled: true, Endpoint: "engage.cloudflareclient.com:2408"}
			state.rules = append(state.rules, model.RoutingRule{Name: "pending-route", Match: "domain:pending.example", Outbound: "warp", Enabled: true})
		}},
	}

	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			router, state := newApplyTrackedRouterWithState(t)
			t.Cleanup(func() { _ = state.Close() })
			inboundResponse := v1Request(t, router, http.MethodPost, "/api/inbounds",
				`{"name":"applied-subscription-hy","protocol":"hysteria2","transport":"udp","port":27443,"enabled":true}`)
			if inboundResponse.Code != http.StatusCreated && inboundResponse.Code != http.StatusOK {
				t.Fatalf("create inbound: %d %s", inboundResponse.Code, inboundResponse.Body.String())
			}
			clientResponse := v1Request(t, router, http.MethodPost, "/api/v1/clients",
				`{"name":"applied-subscription-client","bindings":[{"inboundId":"applied-subscription-hy","runtimeIdentity":"applied_subscription_identity","credential":"applied-subscription-credential"}]}`)
			if clientResponse.Code != http.StatusCreated {
				t.Fatalf("create client: %d %s", clientResponse.Code, clientResponse.Body.String())
			}
			created := unwrapClient(t, clientResponse.Body.Bytes())
			clientID := created["id"].(string)
			bindingID := created["bindings"].([]any)[0].(map[string]any)["id"].(string)
			issued, err := state.tokenStore.Issue(clientID, "applied-state-test", nil)
			if err != nil {
				t.Fatal(err)
			}
			before := publicRawSubscription(t, router, issued.Plaintext)
			if before.Code != http.StatusOK || before.Body.Len() == 0 {
				t.Fatalf("baseline subscription: %d %q", before.Code, before.Body.String())
			}
			revisionsBefore, err := state.applyRevisions.Get()
			if err != nil {
				t.Fatal(err)
			}
			if revisionsBefore.Applied == 0 || revisionsBefore.Desired != revisionsBefore.Applied {
				t.Fatalf("baseline is not applied: %+v", revisionsBefore)
			}

			state.applyRunner = veilapply.NewRunner(state.applyRevisions, state.applyJobs, veilapply.ExecutorFunc(func(uint64) (veilapply.Result, error) {
				return veilapply.Result{Success: false}, errors.New("simulated desired-state apply failure")
			}))
			if mutation.name == "credential" {
				mutation.mutate(t, state, bindingID)
				state.mu.Lock()
			} else {
				state.mu.Lock()
				mutation.mutate(t, state, bindingID)
			}
			if _, err := state.bumpDesiredRevisionLocked(); err != nil {
				state.mu.Unlock()
				t.Fatalf("commit desired snapshot: %v", err)
			}
			state.autoApplyResultLocked(nil, "test")
			state.mu.Unlock()

			revisionsAfter, err := state.applyRevisions.Get()
			if err != nil {
				t.Fatal(err)
			}
			if revisionsAfter.Desired <= revisionsBefore.Desired || revisionsAfter.Applied != revisionsBefore.Applied {
				t.Fatalf("failed desired mutation revisions: before=%+v after=%+v", revisionsBefore, revisionsAfter)
			}
			after := publicRawSubscription(t, router, issued.Plaintext)
			if after.Code != http.StatusOK {
				t.Fatalf("pending subscription status = %d body=%q", after.Code, after.Body.String())
			}
			if after.Body.String() != before.Body.String() {
				t.Errorf("subscription published desired-not-applied %s change\nbefore=%q\nafter=%q", mutation.name, before.Body.String(), after.Body.String())
			}
			if got := after.Header().Get("X-Veil-Configuration-State"); got != "stale" {
				t.Errorf("configuration state header = %q, want stale", got)
			}
			if got := after.Header().Get("X-Veil-Applied-Revision"); got != strconv.FormatUint(revisionsBefore.Applied, 10) {
				t.Errorf("applied revision header = %q, want %d", got, revisionsBefore.Applied)
			}
			assertAuthenticatedLinksMatchAppliedSubscription(t, router, clientID, after.Body.String())
			links := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+clientID+"/links", "")
			if links.Header().Get("X-Veil-Configuration-State") != "stale" {
				t.Errorf("authenticated links configuration state = %q, want stale", links.Header().Get("X-Veil-Configuration-State"))
			}
			switch mutation.name {
			case "credential":
				if strings.Contains(links.Body.String(), "desired-not-applied-credential") {
					t.Errorf("authenticated links published unapplied credential: %s", links.Body.String())
				}
			case "domain":
				if strings.Contains(links.Body.String(), "desired-not-applied.example.net") {
					t.Errorf("authenticated links published unapplied domain: %s", links.Body.String())
				}
			case "protocol":
				if strings.Contains(strings.ToLower(links.Body.String()), "mieru") {
					t.Errorf("authenticated links published unapplied protocol: %s", links.Body.String())
				}
			}
			if got := after.Header().Get("X-Veil-Desired-Revision"); got != strconv.FormatUint(revisionsAfter.Desired, 10) {
				t.Errorf("desired revision header = %q, want %d", got, revisionsAfter.Desired)
			}
			if mutation.name == "warp_routing" && after.Body.String() != before.Body.String() {
				t.Errorf("WARP/routing-only desired change altered subscription")
			}
		})
	}
}

func TestAuthenticatedLinksConvergeAfterSuccessfulApply(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	inboundResponse := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"applied-links-hy","protocol":"hysteria2","transport":"udp","port":27444,"enabled":true}`)
	if inboundResponse.Code != http.StatusCreated && inboundResponse.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inboundResponse.Code, inboundResponse.Body.String())
	}
	clientResponse := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"applied-links-client","bindings":[{"inboundId":"applied-links-hy","runtimeIdentity":"applied_links_identity","credential":"applied-links-credential"}]}`)
	if clientResponse.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", clientResponse.Code, clientResponse.Body.String())
	}
	created := unwrapClient(t, clientResponse.Body.Bytes())
	clientID := created["id"].(string)
	issuedToken, err := state.tokenStore.Issue(clientID, "applied-links-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := v1Request(t, router, http.MethodPut, "/api/settings",
		`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"applied-after-success.example.net"}`)
	if settings.Code != http.StatusOK {
		t.Fatalf("settings: %d %s", settings.Code, settings.Body.String())
	}
	after := publicRawSubscription(t, router, issuedToken.Plaintext)
	if after.Code != http.StatusOK || !strings.Contains(after.Body.String(), "applied-after-success.example.net") {
		t.Fatalf("applied subscription missing new domain: %d %q", after.Code, after.Body.String())
	}
	assertAuthenticatedLinksMatchAppliedSubscription(t, router, clientID, after.Body.String())
}

func assertAuthenticatedLinksMatchAppliedSubscription(t *testing.T, router http.Handler, clientID, rawSubscription string) {
	t.Helper()
	resp := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+clientID+"/links", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("links: %d %s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Items []struct {
			URI string `json:"uri"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if rawSubscription == "" {
		if len(payload.Items) != 0 {
			t.Fatalf("authenticated links = %+v, want empty to match applied subscription", payload.Items)
		}
		return
	}
	if len(payload.Items) == 0 {
		t.Fatalf("authenticated links empty, applied subscription %q", rawSubscription)
	}
	for _, item := range payload.Items {
		if item.URI == "" {
			continue
		}
		if !strings.Contains(rawSubscription, item.URI) {
			t.Errorf("authenticated link %q missing from applied subscription %q", item.URI, rawSubscription)
		}
	}
}
