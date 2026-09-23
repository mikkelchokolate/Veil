package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// Regression for #671: the public subscription feed must gate link delivery
// on LIVE traffic totals, not only the applied snapshot's Depleted flag.
// Between the quota breach and apply convergence the feed already knows the
// client is over quota (Subscription-Userinfo shows it) — it must stop
// serving credentials the same way the wall-clock ExpiresAt check does.
func TestPublicSubscriptionDropsLinksWhenLiveTotalsReachQuota(t *testing.T) {
	r, state := newSubscriptionTestRouter(t)

	inbound := v1Request(t, r, http.MethodPost, "/api/inbounds",
		`{"name":"hy2-quota-feed","protocol":"hysteria2","transport":"udp","port":27446,"enabled":true}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	// The subscription feed reads the APPLIED snapshot: a mutation that
	// returns 201 with success:false (apply job failed) leaves the client out
	// of it and the feed would 404 — assert convergence like
	// seedClientWithToken does instead of racing the snapshot.
	var inboundEnvelope struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(inbound.Body.Bytes(), &inboundEnvelope); err != nil || !inboundEnvelope.Success {
		t.Fatalf("inbound did not apply: decode=%v body=%s", err, inbound.Body.String())
	}
	created := v1Request(t, r, http.MethodPost, "/api/v1/clients",
		`{"name":"quota-feed","quotaBytes":1000,"bindings":[{"inboundId":"hy2-quota-feed","credential":"pw-quota-feed"}]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("client: %d %s", created.Code, created.Body.String())
	}
	// "success" is a top-level envelope field (see seedClientWithToken);
	// tolerate its absence but fail on an explicit false.
	var createEnvelope map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &createEnvelope); err == nil {
		if success, ok := createEnvelope["success"].(bool); ok && !success {
			t.Fatalf("client did not apply: %s", created.Body.String())
		}
	}
	clientID := unwrapClient(t, created.Body.Bytes())["id"].(string)
	tokens := v1Request(t, r, http.MethodPost, "/api/v1/clients/"+clientID+"/tokens", `{"label":"feed"}`)
	if tokens.Code != http.StatusCreated {
		t.Fatalf("token: %d %s", tokens.Code, tokens.Body.String())
	}
	var tok struct {
		Plaintext string `json:"plaintext"`
	}
	if err := json.NewDecoder(tokens.Body).Decode(&tok); err != nil || tok.Plaintext == "" {
		t.Fatalf("token plaintext missing: %v %s", err, tokens.Body.String())
	}

	// Sanity: under-quota the feed serves the credential.
	before := publicRawSubscription(t, r, tok.Plaintext)
	if before.Code != http.StatusOK || !strings.Contains(before.Body.String(), "pw-quota-feed") {
		t.Fatalf("pre-quota subscription must serve links: %d %s", before.Code, before.Body.String())
	}

	// Record live traffic that breaches the quota. The applied snapshot's
	// Depleted stays false — nothing re-applied — so only the live-totals
	// gate can catch this.
	var bindingID string
	if err := state.db.QueryRow(`SELECT id FROM client_bindings WHERE client_id=? LIMIT 1`, clientID).Scan(&bindingID); err != nil {
		t.Fatalf("lookup binding: %v", err)
	}
	if err := state.trafficStore.RecordSample(client.Sample{
		BindingID:     bindingID,
		ClientID:      clientID,
		UploadBytes:   600,
		DownloadBytes: 500,
		AtUnix:        1000,
	}); err != nil {
		t.Fatalf("record traffic: %v", err)
	}

	after := publicRawSubscription(t, r, tok.Plaintext)
	if after.Code != http.StatusOK {
		t.Fatalf("over-quota subscription must still return 200, got %d %s", after.Code, after.Body.String())
	}
	if strings.Contains(after.Body.String(), "pw-quota-feed") {
		t.Fatalf("over-quota client still received credentials: %q", after.Body.String())
	}
	// The metadata must keep reporting the observed totals — the gate is on
	// the body, not on honesty about usage.
	userinfo := after.Header().Get("Subscription-Userinfo")
	if got := headerIntField(userinfo, "upload"); got != 600 {
		t.Fatalf("Subscription-Userinfo upload=%d want 600: %q", got, userinfo)
	}
	if got := headerIntField(userinfo, "download"); got != 500 {
		t.Fatalf("Subscription-Userinfo download=%d want 500: %q", got, userinfo)
	}
	if state := after.Header().Get("X-Veil-Traffic-State"); state != "observed" {
		t.Fatalf("X-Veil-Traffic-State=%q want observed", state)
	}
}
