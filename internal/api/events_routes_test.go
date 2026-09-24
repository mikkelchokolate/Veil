package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// TestV1EventsSSE verifies (A10) that /api/v1/events streams SSE events
// for both traffic and apply types.
func TestV1EventsSSE(t *testing.T) {
	s := &managementState{}
	s.cipher = newTestCipher(t)
	s.settings = Settings{Domain: "x.example"}
	s.inbounds = []Inbound{{Name: "hy2", Protocol: "hysteria2", Enabled: true}}

	db := openApplyTestDB(t)
	repo := client.NewRepository(db)
	creds := client.NewCredentialStore(db, s.cipher)
	svc := client.NewService(repo, creds)
	s.clientService = svc
	s.clientRepo = repo
	s.trafficStore = client.NewTrafficStore(db)

	// Create a client for traffic events.
	view, _ := svc.Create(client.Client{Name: "alice", Enabled: true})
	b, _ := svc.AddBinding(view.ID, "hy2")
	svc.SetCredential(b.ID, "password", "secret")

	mux := http.NewServeMux()
	s.registerEventsRoutes(mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Connect to SSE stream.
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	clientHTTP := &http.Client{Timeout: 3 * time.Second}
	resp, err := clientHTTP.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type %q, want text/event-stream", ct)
	}

	// Read events with timeout.
	events := make(map[string]string)
	scanner := bufio.NewScanner(resp.Body)
	deadline := time.Now().Add(2 * time.Second)
	for scanner.Scan() && time.Now().Before(deadline) {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType := strings.TrimPrefix(line, "event: ")
			// Read next line for data.
			if scanner.Scan() {
				dataLine := scanner.Text()
				if strings.HasPrefix(dataLine, "data: ") {
					events[eventType] = strings.TrimPrefix(dataLine, "data: ")
				}
			}
		}
		if len(events) >= 2 {
			break
		}
	}

	if _, ok := events["traffic"]; !ok {
		t.Error("missing traffic event")
	}
	if _, ok := events["apply"]; !ok {
		t.Error("missing apply event")
	}

	// Verify the traffic event carries real JSON substance — substring checks
	// green an error/message body containing the same words (#871).
	if data, ok := events["traffic"]; ok {
		var traffic struct {
			At      int64 `json:"at"`
			Clients map[string]struct {
				Upload   int64 `json:"upload"`
				Download int64 `json:"download"`
			} `json:"clients"`
		}
		if err := json.Unmarshal([]byte(data), &traffic); err != nil {
			t.Fatalf("traffic event data is not JSON: %v (%q)", err, data)
		}
		entry, ok := traffic.Clients[view.ID]
		if !ok {
			t.Fatalf("traffic event missing created client %s: %q", view.ID, data)
		}
		if entry.Upload != 0 || entry.Download != 0 {
			t.Fatalf("fresh client must report zeroed counters, got %+v", entry)
		}
	}
	// Verify the apply event carries real JSON substance — desiredRevision must
	// be a numeric field, not a substring match (#871).
	if data, ok := events["apply"]; ok {
		var applyEvent map[string]any
		if err := json.Unmarshal([]byte(data), &applyEvent); err != nil {
			t.Fatalf("apply event data is not JSON: %v (%q)", err, data)
		}
		if _, ok := applyEvent["desiredRevision"].(float64); !ok {
			t.Fatalf("apply event desiredRevision is not a number: %q", data)
		}
		if _, ok := applyEvent["appliedRevision"].(float64); !ok {
			t.Fatalf("apply event appliedRevision is not a number: %q", data)
		}
		if _, ok := applyEvent["state"].(string); !ok {
			t.Fatalf("apply event missing state field: %q", data)
		}
	}
}

// TestV1EventsSSETypeFilter verifies that ?types= filters event types.
func TestV1EventsSSETypeFilter(t *testing.T) {
	s := &managementState{}
	s.cipher = newTestCipher(t)
	s.settings = Settings{Domain: "x.example"}
	db := openApplyTestDB(t)
	s.trafficStore = client.NewTrafficStore(db)
	s.clientService = client.NewService(client.NewRepository(db), client.NewCredentialStore(db, s.cipher))

	mux := http.NewServeMux()
	s.registerEventsRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Request only traffic events.
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/events?types=traffic", nil)
	clientHTTP := &http.Client{Timeout: 3 * time.Second}
	resp, err := clientHTTP.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	events := make(map[string]bool)
	scanner := bufio.NewScanner(resp.Body)
	deadline := time.Now().Add(2 * time.Second)
	for scanner.Scan() && time.Now().Before(deadline) {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			events[strings.TrimPrefix(line, "event: ")] = true
		}
		if len(events) >= 2 {
			break
		}
	}

	if !events["traffic"] {
		t.Error("missing traffic event")
	}
	if events["apply"] {
		t.Error("apply event should be filtered out by ?types=traffic")
	}
}

// TestV1EventsMethodNotAllowed verifies non-GET requests are rejected.
func TestV1EventsMethodNotAllowed(t *testing.T) {
	s := &managementState{}
	mux := http.NewServeMux()
	s.registerEventsRoutes(mux)

	req := httptest.NewRequest("POST", "/api/v1/events", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/v1/events: status %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}
