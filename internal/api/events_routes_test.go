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

	// The traffic payload must be real JSON carrying the per-client totals map,
	// and the created client's ID must be a key — a bare "clients" substring is
	// not proof of a well-formed payload.
	var traffic struct {
		At      int64 `json:"at"`
		Clients map[string]struct {
			Upload   int64 `json:"upload"`
			Download int64 `json:"download"`
		} `json:"clients"`
	}
	if data, ok := events["traffic"]; ok {
		if err := json.Unmarshal([]byte(data), &traffic); err != nil {
			t.Fatalf("traffic event data is not valid JSON: %v (%s)", err, data)
		}
		if traffic.At == 0 {
			t.Errorf("traffic event missing at timestamp: %s", data)
		}
		if _, ok := traffic.Clients[view.ID]; !ok {
			t.Errorf("traffic event clients map lacks created client %q: %s", view.ID, data)
		} else if c := traffic.Clients[view.ID]; c.Upload != 0 || c.Download != 0 {
			t.Errorf("fresh client must report zeroed counters: %+v", c)
		}
	}
	// The apply payload must decode to a revision object that actually
	// carries numeric revision fields and a state — not merely contain the
	// key name as a substring.
	if data, ok := events["apply"]; ok {
		var apply map[string]any
		if err := json.Unmarshal([]byte(data), &apply); err != nil {
			t.Fatalf("apply event data is not valid JSON: %v (%s)", err, data)
		}
		if state, _ := apply["state"].(string); state == "" {
			t.Errorf("apply event missing state field: %s", data)
		}
		for _, field := range []string{"desiredRevision", "appliedRevision"} {
			v, ok := apply[field]
			if !ok {
				t.Errorf("apply event missing %s field: %s", field, data)
				continue
			}
			if _, ok := v.(float64); !ok {
				t.Errorf("apply event %s is not numeric: %s", field, data)
			}
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
