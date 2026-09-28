package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestV1ClientLinksAggregatesMieruTransportBindings(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	t.Cleanup(func() { _ = state.Close() })
	tcp := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"mieru-tcp-link","protocol":"mieru","transport":"tcp","port":2443,"enabled":true,"password":"inbound-pass"}`)
	if tcp.Code != http.StatusCreated && tcp.Code != http.StatusOK {
		t.Fatalf("tcp inbound: %d %s", tcp.Code, tcp.Body.String())
	}
	udp := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"mieru-udp-link","protocol":"mieru","transport":"udp","port":2443,"enabled":true,"password":"inbound-pass"}`)
	if udp.Code != http.StatusCreated && udp.Code != http.StatusOK {
		t.Fatalf("udp inbound: %d %s", udp.Code, udp.Body.String())
	}
	// Both bindings carry the SAME credential (identity + password), so they
	// aggregate into one link — a per-inbound NOCASE index permits reusing an
	// identity across inbounds (#1121).
	created := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"mieru-link-client","bindings":[{"inboundId":"mieru-tcp-link","runtimeIdentity":"shared_mieru","credential":"alice-pass"},{"inboundId":"mieru-udp-link","runtimeIdentity":"shared_mieru","credential":"alice-pass"}]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	id := unwrapClient(t, created.Body.Bytes())["id"].(string)
	// The links endpoint serves the applied snapshot, which lands
	// asynchronously and may need several revisions before this client's
	// bindings are rendered. Poll at a slow cadence (fast polling trips the
	// endpoint's rate limiter); 429s are retryable within the deadline.
	var linksResp *httptest.ResponseRecorder
	var probe struct {
		Items []struct {
			Protocol string `json:"protocol"`
		} `json:"items"`
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		linksResp = v1Request(t, router, http.MethodGet, "/api/v1/clients/"+id+"/links", "")
		if linksResp.Code == http.StatusOK {
			probe.Items = nil
			if err := json.Unmarshal(linksResp.Body.Bytes(), &probe); err == nil {
				found := false
				for _, item := range probe.Items {
					if item.Protocol == "mieru" {
						found = true
					}
				}
				if found {
					break
				}
			}
		} else if linksResp.Code != http.StatusTooManyRequests {
			t.Fatalf("links: %d %s", linksResp.Code, linksResp.Body.String())
		}
		if time.Now().After(deadline) {
			rev, _ := state.applyRevisions.Get()
			t.Fatalf("links never contained mieru items: last=%d %s (rev=%+v)", linksResp.Code, linksResp.Body.String(), rev)
		}
		time.Sleep(1500 * time.Millisecond)
	}
	var body struct {
		Items []struct {
			Protocol string `json:"protocol"`
			URI      string `json:"uri"`
			Config   string `json:"config"`
		} `json:"items"`
	}
	if err := json.Unmarshal(linksResp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var mieru []struct {
		Protocol string `json:"protocol"`
		URI      string `json:"uri"`
		Config   string `json:"config"`
	}
	for _, item := range body.Items {
		if item.Protocol == "mieru" {
			mieru = append(mieru, item)
		}
	}
	if len(mieru) != 1 {
		t.Fatalf("mieru links=%d, want 1 aggregated profile: %+v", len(mieru), body.Items)
	}
	if strings.Count(mieru[0].URI, "port=2443") != 2 || !strings.Contains(mieru[0].URI, "protocol=TCP") || !strings.Contains(mieru[0].URI, "protocol=UDP") {
		t.Fatalf("aggregated URI = %q", mieru[0].URI)
	}
	var config struct {
		Profiles []struct {
			Servers []struct {
				PortBindings []struct {
					Protocol string `json:"protocol"`
				} `json:"portBindings"`
			} `json:"servers"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(mieru[0].Config), &config); err != nil {
		t.Fatalf("config: %v\n%s", err, mieru[0].Config)
	}
	if len(config.Profiles) != 1 || len(config.Profiles[0].Servers) != 1 || len(config.Profiles[0].Servers[0].PortBindings) != 2 {
		t.Fatalf("aggregated config = %+v", config)
	}
}
