package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func hysteria2URIFromLinks(t *testing.T, router http.Handler, clientID string) string {
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
	for _, item := range payload.Items {
		if strings.Contains(item.URI, "hysteria2://") {
			return item.URI
		}
	}
	t.Fatalf("no hysteria2 link in %s", resp.Body.String())
	return ""
}

func rawSubscriptionBody(t *testing.T, router http.Handler, token string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/s/"+token+"?format=raw", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("subscription: %d %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func TestPerClientExportHonorsGlobalHysteria2Insecure(t *testing.T) {
	router, _ := newSubscriptionTestRouter(t)
	token, clientID := seedClientWithToken(t, router)
	settings := v1Request(t, router, http.MethodPut, "/api/settings",
		`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com","hysteria2Insecure":true}`)
	if settings.Code != http.StatusOK {
		t.Fatalf("settings: %d %s", settings.Code, settings.Body.String())
	}

	link := hysteria2URIFromLinks(t, router, clientID)
	if !strings.Contains(link, "insecure=1") {
		t.Fatalf("per-client links omitted global insecure opt-in: %s", link)
	}
	body := rawSubscriptionBody(t, router, token)
	if !strings.Contains(body, "insecure=1") {
		t.Fatalf("applied subscription omitted global insecure opt-in: %s", body)
	}
}

func TestPerClientExportHonorsInboundHysteria2Insecure(t *testing.T) {
	router, _ := newSubscriptionTestRouter(t)
	inbound := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"hy2-insecure","protocol":"hysteria2","transport":"udp","port":9444,"enabled":true,"hysteria2Insecure":true}`)
	if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
		t.Fatalf("inbound: %d %s", inbound.Code, inbound.Body.String())
	}
	created := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"inbound-insecure","bindings":[{"inboundId":"hy2-insecure","credential":"pw-insecure"}]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("client: %d %s", created.Code, created.Body.String())
	}
	clientID := unwrapClient(t, created.Body.Bytes())["id"].(string)
	issued := v1Request(t, router, http.MethodPost, "/api/v1/clients/"+clientID+"/tokens", `{"label":"phone"}`)
	var tok struct {
		Plaintext string `json:"plaintext"`
	}
	if err := json.Unmarshal(issued.Body.Bytes(), &tok); err != nil || tok.Plaintext == "" {
		t.Fatalf("token: %v %s", err, issued.Body.String())
	}

	link := hysteria2URIFromLinks(t, router, clientID)
	if !strings.Contains(link, "insecure=1") {
		t.Fatalf("inbound insecure flag dropped from links: %s", link)
	}
	if !strings.Contains(rawSubscriptionBody(t, router, tok.Plaintext), "insecure=1") {
		t.Fatal("inbound insecure flag dropped from subscription")
	}
}

func TestPerClientExportOmitsInsecureWhenUnset(t *testing.T) {
	router, _ := newSubscriptionTestRouter(t)
	token, clientID := seedClientWithToken(t, router)
	link := hysteria2URIFromLinks(t, router, clientID)
	if strings.Contains(link, "insecure=1") {
		t.Fatalf("secure default leaked insecure=1: %s", link)
	}
	if strings.Contains(rawSubscriptionBody(t, router, token), "insecure=1") {
		t.Fatal("subscription leaked insecure=1 with secure default")
	}
}

func TestPerClientExportHonorsProtocolFieldsHysteria2Insecure(t *testing.T) {
	router, _ := newSubscriptionTestRouter(t)
	token, clientID := seedClientWithToken(t, router)
	settings := v1Request(t, router, http.MethodPut, "/api/settings",
		`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com","protocolFields":{"hysteria2Insecure":true}}`)
	if settings.Code != http.StatusOK {
		t.Fatalf("settings: %d %s", settings.Code, settings.Body.String())
	}
	link := hysteria2URIFromLinks(t, router, clientID)
	if !strings.Contains(link, "insecure=1") {
		t.Fatalf("protocolFields insecure opt-in dropped from links: %s", link)
	}
	if !strings.Contains(rawSubscriptionBody(t, router, token), "insecure=1") {
		t.Fatal("protocolFields insecure opt-in dropped from subscription")
	}
}
