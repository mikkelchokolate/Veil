package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestPublicSubscriptionURLDirectIPv4PreservesPort(t *testing.T) {
	got := publicSubscriptionURL(Settings{
		PanelAccess: "direct",
		PanelListen: "0.0.0.0:44375",
		Domain:      "192.0.2.20",
	}, "dummy-test-token")
	want := "https://192.0.2.20:44375/s/dummy-test-token"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPublicSubscriptionURLDirectIPv6BracketsPort(t *testing.T) {
	got := publicSubscriptionURL(Settings{
		PanelAccess: "direct",
		PanelListen: "[::]:44375",
		Domain:      "2001:db8::20",
	}, "dummy-test-token")
	want := "https://[2001:db8::20]:44375/s/dummy-test-token"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPublicSubscriptionURLCaddyOmitsDefaultHTTPSPort(t *testing.T) {
	got := publicSubscriptionURL(Settings{
		PanelAccess:     "caddy",
		PanelListen:     "127.0.0.1:2096",
		Domain:          "vpn.example.com",
		PanelPublicPort: 443,
	}, "dummy-test-token")
	want := "https://vpn.example.com/s/dummy-test-token"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPublicSubscriptionURLCaddyUsesPanelDomain(t *testing.T) {
	got := publicSubscriptionURL(Settings{
		PanelAccess:     "caddy",
		PanelListen:     "127.0.0.1:2096",
		Domain:          "vpn.example.com",
		PanelDomain:     "panel.example.com",
		PanelPublicPort: 443,
	}, "dummy-test-token")
	want := "https://panel.example.com/s/dummy-test-token"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPublicSubscriptionURLCaddyPanelDomainCustomPublicPort(t *testing.T) {
	got := publicSubscriptionURL(Settings{
		PanelAccess:     "caddy",
		PanelListen:     "127.0.0.1:2096",
		Domain:          "vpn.example.com",
		PanelDomain:     "panel.example.com",
		PanelPublicPort: 8443,
	}, "dummy-test-token")
	want := "https://panel.example.com:8443/s/dummy-test-token"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPublicSubscriptionURLCaddyCustomPublicPort(t *testing.T) {
	got := publicSubscriptionURL(Settings{
		PanelAccess:     "caddy",
		PanelListen:     "127.0.0.1:2096",
		Domain:          "vpn.example.com",
		PanelPublicPort: 8443,
	}, "dummy-test-token")
	want := "https://vpn.example.com:8443/s/dummy-test-token"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPublicSubscriptionURLCaddyIPv6OmitsDefaultPort(t *testing.T) {
	got := publicSubscriptionURL(Settings{
		PanelAccess: "caddy",
		PanelListen: "127.0.0.1:2096",
		Domain:      "2001:db8::20",
	}, "dummy-test-token")
	want := "https://[2001:db8::20]/s/dummy-test-token"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPublicSubscriptionURLRelativeWithoutDomain(t *testing.T) {
	got := publicSubscriptionURL(Settings{PanelAccess: "direct", PanelListen: "0.0.0.0:44375"}, "dummy-test-token")
	if got != "/s/dummy-test-token" {
		t.Fatalf("got %q", got)
	}
}

func TestSubscriptionTokenIssueListRevealRotateUsePublicOrigin(t *testing.T) {
	router, state := newSubscriptionTestRouter(t)
	_, clientID := seedClientWithToken(t, router)

	state.mu.Lock()
	state.settings.PanelAccess = "direct"
	state.settings.PanelListen = "0.0.0.0:44375"
	state.settings.Domain = "192.0.2.20"
	state.mu.Unlock()

	issued := v1Request(t, router, http.MethodPost, "/api/v1/clients/"+clientID+"/tokens", `{"label":"laptop"}`)
	if issued.Code != http.StatusCreated {
		t.Fatalf("issue: %d %s", issued.Code, issued.Body.String())
	}
	var created struct {
		Plaintext string `json:"plaintext"`
		URL       string `json:"url"`
		Token     struct {
			ID string `json:"id"`
		} `json:"token"`
	}
	if err := json.Unmarshal(issued.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	wantPrefix := "https://192.0.2.20:44375/s/"
	if !strings.HasPrefix(created.URL, wantPrefix) || !strings.Contains(created.URL, created.Plaintext) {
		t.Fatalf("issued url = %q", created.URL)
	}

	listed := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+clientID+"/tokens", "")
	var list struct {
		Items []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range list.Items {
		if item.ID == created.Token.ID {
			found = true
			if !strings.HasPrefix(item.URL, wantPrefix) {
				t.Fatalf("listed url = %q", item.URL)
			}
		}
	}
	if !found {
		t.Fatalf("issued token missing from list: %s", listed.Body.String())
	}

	revealed := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+clientID+"/tokens/"+created.Token.ID, "")
	if !strings.Contains(revealed.Body.String(), wantPrefix) {
		t.Fatalf("reveal url missing origin: %s", revealed.Body.String())
	}

	rotated := v1Request(t, router, http.MethodPost, "/api/v1/clients/"+clientID+"/tokens/"+created.Token.ID+"/rotate", `{}`)
	if rotated.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", rotated.Code, rotated.Body.String())
	}
	if !strings.Contains(rotated.Body.String(), wantPrefix) {
		t.Fatalf("rotate url missing origin: %s", rotated.Body.String())
	}
}

func TestSubscriptionTokenIssueListRevealRotateUsePanelDomain(t *testing.T) {
	router, state := newSubscriptionTestRouter(t)
	_, clientID := seedClientWithToken(t, router)

	state.mu.Lock()
	state.settings.PanelAccess = "caddy"
	state.settings.PanelListen = "127.0.0.1:2096"
	state.settings.Domain = "vpn.example.com"
	state.settings.PanelDomain = "panel.example.com"
	state.settings.PanelPublicPort = 443
	state.mu.Unlock()

	issued := v1Request(t, router, http.MethodPost, "/api/v1/clients/"+clientID+"/tokens", `{"label":"laptop"}`)
	if issued.Code != http.StatusCreated {
		t.Fatalf("issue: %d %s", issued.Code, issued.Body.String())
	}
	var created struct {
		Plaintext string `json:"plaintext"`
		URL       string `json:"url"`
		Token     struct {
			ID string `json:"id"`
		} `json:"token"`
	}
	if err := json.Unmarshal(issued.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	wantPrefix := "https://panel.example.com/s/"
	if !strings.HasPrefix(created.URL, wantPrefix) || !strings.Contains(created.URL, created.Plaintext) {
		t.Fatalf("issued url = %q", created.URL)
	}
	if strings.Contains(created.URL, "vpn.example.com") {
		t.Fatalf("issued url used inbound domain: %q", created.URL)
	}

	listed := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+clientID+"/tokens", "")
	if !strings.Contains(listed.Body.String(), wantPrefix) {
		t.Fatalf("list url missing panel domain: %s", listed.Body.String())
	}

	revealed := v1Request(t, router, http.MethodGet, "/api/v1/clients/"+clientID+"/tokens/"+created.Token.ID, "")
	if !strings.Contains(revealed.Body.String(), wantPrefix) {
		t.Fatalf("reveal url missing panel domain: %s", revealed.Body.String())
	}

	rotated := v1Request(t, router, http.MethodPost, "/api/v1/clients/"+clientID+"/tokens/"+created.Token.ID+"/rotate", `{}`)
	if rotated.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", rotated.Code, rotated.Body.String())
	}
	if !strings.Contains(rotated.Body.String(), wantPrefix) {
		t.Fatalf("rotate url missing panel domain: %s", rotated.Body.String())
	}
}
