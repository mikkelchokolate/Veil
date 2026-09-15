package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestV1TrafficClientCollectedAtIsObservationTime(t *testing.T) {
	router, state := newTrafficRouter(t)
	id := seedTrafficClient(t, router, state, "freshness", 40, 60)
	var bindingID string
	if err := state.db.QueryRow(`SELECT id FROM client_bindings WHERE client_id=?`, id).Scan(&bindingID); err != nil {
		t.Fatal(err)
	}
	const observedAt int64 = 1_700_000_333
	if err := state.trafficStore.RecordSample(client.Sample{
		BindingID: bindingID, UploadBytes: 5, DownloadBytes: 7, AtUnix: observedAt,
	}); err != nil {
		t.Fatal(err)
	}

	first := decodeClientTraffic(t, v1Request(t, router, http.MethodGet, "/api/v1/traffic/"+id, ""))
	if first.CollectedAt == nil || *first.CollectedAt != observedAt {
		t.Fatalf("collectedAt=%v, want %d", first.CollectedAt, observedAt)
	}
	if first.State != "healthy" && first.State != "stale" {
		t.Fatalf("state=%q after observation", first.State)
	}
	second := decodeClientTraffic(t, v1Request(t, router, http.MethodGet, "/api/v1/traffic/"+id, ""))
	if second.CollectedAt == nil || *second.CollectedAt != observedAt {
		t.Fatalf("second collectedAt=%v, request clock leaked", second.CollectedAt)
	}
}

func TestV1TrafficClientUnsupportedHasNoCollectedAt(t *testing.T) {
	router, _ := newTrafficRouter(t)
	inbound := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"in-naive-obs","protocol":"naiveproxy","transport":"tcp","port":18443,"enabled":true,"protocolFields":{"domain":"vpn.example.com"}}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	created := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"naive-obs","bindings":[{"inboundId":"in-naive-obs","credential":"pw"}]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	id := unwrapClient(t, created.Body.Bytes())["id"].(string)
	body := decodeClientTraffic(t, v1Request(t, router, http.MethodGet, "/api/v1/traffic/"+id, ""))
	if body.CollectedAt != nil {
		t.Fatalf("unsupported collectedAt=%v, want null", body.CollectedAt)
	}
	if body.State != "unsupported" {
		t.Fatalf("state=%q, want unsupported", body.State)
	}
	if body.UploadBytes != 0 || body.DownloadBytes != 0 {
		t.Fatalf("unsupported totals=%+v", body)
	}
}

func TestV1TrafficClientStaleWhenProviderDegraded(t *testing.T) {
	router, state := newTrafficRouter(t)
	id := seedTrafficClient(t, router, state, "stale-obs", 8, 9)
	provider := &healthRegressionTrafficProvider{key: "stale-provider", err: errors.New("collector down")}
	if err := state.trafficCollector.ResetProviders([]client.TrafficProvider{provider}); err != nil {
		t.Fatal(err)
	}
	if err := state.trafficCollector.CollectOnce(); err == nil {
		t.Fatal("expected provider failure")
	}
	body := decodeClientTraffic(t, v1Request(t, router, http.MethodGet, "/api/v1/traffic/"+id, ""))
	if body.CollectedAt == nil {
		t.Fatal("stale totals must retain last observation time")
	}
	if body.State != "stale" {
		t.Fatalf("state=%q, want stale", body.State)
	}
}

type clientTrafficBody struct {
	UploadBytes   int64  `json:"uploadBytes"`
	DownloadBytes int64  `json:"downloadBytes"`
	State         string `json:"state"`
	CollectedAt   *int64 `json:"collectedAt"`
}

func decodeClientTraffic(t *testing.T, rec *httptest.ResponseRecorder) clientTrafficBody {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("traffic: %d %s", rec.Code, rec.Body.String())
	}
	var body clientTrafficBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}
