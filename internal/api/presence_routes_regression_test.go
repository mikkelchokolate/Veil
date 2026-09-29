package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/testutil/testdb"
)

// presenceFakeProvider serves deterministic collector batches for presence
// tests. online is passed through verbatim so nil (no table / failed read)
// vs an empty authoritative table stays distinguishable.
type presenceFakeProvider struct {
	key        string
	readings   map[string]client.ProviderReading
	online     map[string]int64
	observedAt time.Time
	err        error
}

func (p *presenceFakeProvider) Key() string { return p.key }
func (p *presenceFakeProvider) Read() (client.ProviderBatch, error) {
	if p.err != nil {
		return client.ProviderBatch{}, p.err
	}
	observed := p.observedAt
	if observed.IsZero() {
		observed = time.Now().UTC()
	}
	readings := make([]client.ProviderReading, 0, len(p.readings))
	for _, reading := range p.readings {
		readings = append(readings, reading)
	}
	return client.ProviderBatch{
		Readings: readings, ObservedAt: observed,
		RuntimeInstance: p.key, Online: p.online,
	}, nil
}

func mustCreatePresenceClient(t *testing.T, state *managementState, name string, bindings ...client.Binding) client.Client {
	t.Helper()
	return mustCreatePresenceClientRow(t, state,
		client.Client{Name: name, Enabled: true, QuotaResetPolicy: client.ResetNever}, bindings...)
}

func mustCreatePresenceClientRow(t *testing.T, state *managementState, c client.Client, bindings ...client.Binding) client.Client {
	t.Helper()
	row, err := state.clientRepo.Create(c)
	if err != nil {
		t.Fatalf("create client %q: %v", c.Name, err)
	}
	for _, binding := range bindings {
		binding.ClientID = row.ID
		if _, err := state.clientRepo.CreateBinding(binding); err != nil {
			t.Fatalf("create binding for %q: %v", c.Name, err)
		}
	}
	return row
}

func presenceRequest(t *testing.T, state *managementState) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/presence", nil)
	rec := httptest.NewRecorder()
	state.handleV1Presence(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/presence = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode presence response: %v", err)
	}
	return payload
}

func presenceItemsByClientID(t *testing.T, payload map[string]any) []presenceItem {
	t.Helper()
	raw, ok := payload["items"]
	if !ok {
		t.Fatal("presence response has no items field")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var items []presenceItem
	if err := json.Unmarshal(encoded, &items); err != nil {
		t.Fatalf("decode presence items: %v", err)
	}
	return items
}

func presenceItemByClientID(t *testing.T, items []presenceItem, id string) presenceItem {
	t.Helper()
	for _, item := range items {
		if item.ClientID == id {
			return item
		}
	}
	t.Fatalf("no presence item for client %s in %+v", id, items)
	return presenceItem{}
}

// TestV1PresenceAuthGate drives the real mux + auth middleware: anonymous
// callers get 401, a viewer session gets 200 (the endpoint is
// viewer-capable), and unsupported methods stay gated then hit the handler's
// 405.
func TestV1PresenceAuthGate(t *testing.T) {
	db := testdb.Open(t)
	defer db.Close()
	state := &managementState{
		sessions: mustNewSessionRegistry(""),
		users: []User{
			{Username: "admin", PasswordHash: "admin-hash", Role: "admin", Locale: "en"},
			{Username: "viewer", PasswordHash: "viewer-hash", Role: "viewer", Locale: "en"},
		},
		clientRepo:   client.NewRepository(db),
		trafficStore: client.NewTrafficStore(db),
	}
	viewer := mustCreateSession(t, state.sessions, "viewer", "viewer")
	admin := mustCreateSession(t, state.sessions, "admin", "admin")
	mux := http.NewServeMux()
	state.register(mux)
	router := authMiddleware(state, "", mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/presence", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /api/v1/presence = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}

	for _, session := range []Session{viewer, admin} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/presence", nil)
		req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s GET /api/v1/presence = %d, want 200; body=%s", session.Role, rec.Code, rec.Body.String())
		}
		var payload map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if _, ok := payload["items"]; !ok {
			t.Fatalf("%s response has no items field: %s", session.Role, rec.Body.String())
		}
		if payload["count"] != float64(0) {
			t.Fatalf("%s count=%v, want 0 for empty client set", session.Role, payload["count"])
		}
	}

	// An unsupported method is classified adminMutation (never inherits the
	// GET's viewer capability), then the handler answers 405.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/presence", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: viewer.Token})
	req.Header.Set("X-CSRF-Token", viewer.CSRFToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer POST /api/v1/presence = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/presence", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: admin.Token})
	req.Header.Set("X-CSRF-Token", admin.CSRFToken)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("admin POST /api/v1/presence = %d, want 405; body=%s", rec.Code, rec.Body.String())
	}
}

// TestV1PresenceOrderingAndUnsupportedHonesty covers the wire contract:
// deterministic clientId ordering, one item per client (including clients
// with no bindings), and a literal "online": null for bindings whose
// protocol exposes no telemetry.
func TestV1PresenceOrderingAndUnsupportedHonesty(t *testing.T) {
	state := newClientLifecycleTestState(t)
	state.mu.Lock()
	state.inbounds = []Inbound{{Name: "naive-in", Protocol: "naiveproxy", Enabled: true}}
	state.mu.Unlock()

	naiveA := mustCreatePresenceClient(t, state, "naive-a", client.Binding{InboundID: "naive-in", Enabled: true})
	unbound := mustCreatePresenceClient(t, state, "unbound")
	naiveB := mustCreatePresenceClient(t, state, "naive-b", client.Binding{InboundID: "naive-in", Enabled: true})

	payload := presenceRequest(t, state)
	if payload["count"] != float64(3) {
		t.Fatalf("count=%v, want 3", payload["count"])
	}
	items := presenceItemsByClientID(t, payload)

	wantIDs := []string{naiveA.ID, unbound.ID, naiveB.ID}
	sort.Strings(wantIDs)
	gotIDs := []string{items[0].ClientID, items[1].ClientID, items[2].ClientID}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("item order %v, want clientId-sorted %v", gotIDs, wantIDs)
	}
	names := map[string]string{naiveA.ID: "naive-a", unbound.ID: "unbound", naiveB.ID: "naive-b"}
	for _, item := range items {
		if item.Name != names[item.ClientID] {
			t.Fatalf("item %s name=%q, want %q", item.ClientID, item.Name, names[item.ClientID])
		}
		if item.Online != nil {
			t.Fatalf("item %s online=%v, want null (unsupported)", item.ClientID, *item.Online)
		}
		if item.Source != presenceSourceUnsupported {
			t.Fatalf("item %s source=%q, want unsupported", item.ClientID, item.Source)
		}
		if item.Connections != nil || item.LastActiveAt != nil {
			t.Fatalf("item %s carries telemetry fields on an unsupported source: %+v", item.ClientID, item)
		}
	}
}

// TestV1PresenceWireShapeNullOnline locks the literal null on the wire: the
// tri-state must serialize as "online":null, never a fabricated false and
// never a missing key.
func TestV1PresenceWireShapeNullOnline(t *testing.T) {
	state := newClientLifecycleTestState(t)
	mustCreatePresenceClient(t, state, "unbound")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/presence", nil)
	rec := httptest.NewRecorder()
	state.handleV1Presence(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []map[string]json.RawMessage `json:"items"`
		Count int                          `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Count != 1 || len(payload.Items) != 1 {
		t.Fatalf("count=%d items=%d, want 1/1; body=%s", payload.Count, len(payload.Items), rec.Body.String())
	}
	raw, ok := payload.Items[0]["online"]
	if !ok {
		t.Fatalf("item has no online key: %s", rec.Body.String())
	}
	if string(raw) != "null" {
		t.Fatalf("online=%s, want literal null", raw)
	}
	for _, key := range []string{"clientId", "name", "source"} {
		if _, ok := payload.Items[0][key]; !ok {
			t.Fatalf("item missing key %q: %s", key, rec.Body.String())
		}
	}
	if _, ok := payload.Items[0]["connections"]; ok {
		t.Fatalf("unsupported item carries connections: %s", rec.Body.String())
	}
	if _, ok := payload.Items[0]["lastActiveAt"]; ok {
		t.Fatalf("unsupported item carries lastActiveAt: %s", rec.Body.String())
	}
}

// TestV1PresenceHysteria2StatsSource: a fresh /online table answers
// authoritatively — connections flow through, online=true, lastActiveAt
// reflects the live sighting even before counters tick.
func TestV1PresenceHysteria2StatsSource(t *testing.T) {
	state := newClientLifecycleTestState(t)
	state.mu.Lock()
	state.inbounds = []Inbound{{Name: "hy-main", Protocol: "hysteria2", Enabled: true}}
	state.mu.Unlock()
	row := mustCreatePresenceClient(t, state, "hy-client",
		client.Binding{InboundID: "hy-main", Enabled: true})
	bindings, err := state.clientRepo.AllBindings()
	if err != nil {
		t.Fatal(err)
	}
	bindingID := bindings[0].ID

	provider := &presenceFakeProvider{
		key:      "hysteria2:hy-main",
		readings: map[string]client.ProviderReading{bindingID: {BindingID: bindingID, UploadBytes: 100, DownloadBytes: 50}},
		online:   map[string]int64{bindingID: 3},
	}
	if err := state.trafficCollector.ResetProviders([]client.TrafficProvider{provider}); err != nil {
		t.Fatal(err)
	}
	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("collect: %v", err)
	}

	item := presenceItemByClientID(t, presenceItemsByClientID(t, presenceRequest(t, state)), row.ID)
	if item.Online == nil || !*item.Online {
		t.Fatalf("online=%v, want true from stats", item.Online)
	}
	if item.Source != presenceSourceStats {
		t.Fatalf("source=%q, want stats", item.Source)
	}
	if item.Connections == nil || *item.Connections != 3 {
		t.Fatalf("connections=%v, want 3", item.Connections)
	}
	if item.LastActiveAt == nil || time.Now().Unix()-*item.LastActiveAt > 10 {
		t.Fatalf("lastActiveAt=%v, want a live sighting timestamp near now", item.LastActiveAt)
	}
}

// TestV1PresenceHysteria2StatsEmptyTable: an authoritative empty /online map
// is a proven "offline", not an unknown.
func TestV1PresenceHysteria2StatsEmptyTable(t *testing.T) {
	state := newClientLifecycleTestState(t)
	state.mu.Lock()
	state.inbounds = []Inbound{{Name: "hy-main", Protocol: "hysteria2", Enabled: true}}
	state.mu.Unlock()
	row := mustCreatePresenceClient(t, state, "hy-client",
		client.Binding{InboundID: "hy-main", Enabled: true})

	provider := &presenceFakeProvider{key: "hysteria2:hy-main", online: map[string]int64{}}
	if err := state.trafficCollector.ResetProviders([]client.TrafficProvider{provider}); err != nil {
		t.Fatal(err)
	}
	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("collect: %v", err)
	}

	item := presenceItemByClientID(t, presenceItemsByClientID(t, presenceRequest(t, state)), row.ID)
	if item.Online == nil || *item.Online {
		t.Fatalf("online=%v, want false from an authoritative empty table", item.Online)
	}
	if item.Source != presenceSourceStats {
		t.Fatalf("source=%q, want stats", item.Source)
	}
	if item.Connections == nil || *item.Connections != 0 {
		t.Fatalf("connections=%v, want explicit 0 from stats", item.Connections)
	}
}

// TestV1PresenceHysteria2StatsAbsentFallsBackToActivity: when /online does
// not answer (nil table) the collector still records counter growth, so the
// activity heuristic is the honest fallback rather than a blind "offline".
func TestV1PresenceHysteria2StatsAbsentFallsBackToActivity(t *testing.T) {
	state := newClientLifecycleTestState(t)
	state.mu.Lock()
	state.inbounds = []Inbound{{Name: "hy-main", Protocol: "hysteria2", Enabled: true}}
	state.mu.Unlock()
	row := mustCreatePresenceClient(t, state, "hy-client",
		client.Binding{InboundID: "hy-main", Enabled: true})
	bindings, err := state.clientRepo.AllBindings()
	if err != nil {
		t.Fatal(err)
	}
	bindingID := bindings[0].ID

	provider := &presenceFakeProvider{
		key:      "hysteria2:hy-main",
		readings: map[string]client.ProviderReading{bindingID: {BindingID: bindingID, UploadBytes: 100, DownloadBytes: 0}},
		// Online stays nil: the presence read failed / listener has no table.
	}
	if err := state.trafficCollector.ResetProviders([]client.TrafficProvider{provider}); err != nil {
		t.Fatal(err)
	}
	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("baseline collect: %v", err)
	}
	provider.readings[bindingID] = client.ProviderReading{BindingID: bindingID, UploadBytes: 250, DownloadBytes: 0}
	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("second collect: %v", err)
	}

	item := presenceItemByClientID(t, presenceItemsByClientID(t, presenceRequest(t, state)), row.ID)
	if item.Online == nil || !*item.Online {
		t.Fatalf("online=%v, want true from the activity fallback", item.Online)
	}
	if item.Source != presenceSourceActivity {
		t.Fatalf("source=%q, want activity", item.Source)
	}
	if item.Connections != nil {
		t.Fatalf("connections=%v, want absent without a stats source", *item.Connections)
	}
	if item.LastActiveAt == nil || time.Now().Unix()-*item.LastActiveAt > 10 {
		t.Fatalf("lastActiveAt=%v, want a fresh counter-increase timestamp", item.LastActiveAt)
	}
}

// TestV1PresenceHysteria2StatsStaleTable: a session table older than the
// staleness bound must not produce any verdict — the binding falls back to
// activity, and with a healthy-but-quiet provider that means proven offline,
// never a stale "online".
func TestV1PresenceHysteria2StatsStaleTable(t *testing.T) {
	state := newClientLifecycleTestState(t)
	state.mu.Lock()
	state.inbounds = []Inbound{{Name: "hy-main", Protocol: "hysteria2", Enabled: true}}
	state.mu.Unlock()
	row := mustCreatePresenceClient(t, state, "hy-client",
		client.Binding{InboundID: "hy-main", Enabled: true})
	bindings, err := state.clientRepo.AllBindings()
	if err != nil {
		t.Fatal(err)
	}
	bindingID := bindings[0].ID

	provider := &presenceFakeProvider{
		key:        "hysteria2:hy-main",
		observedAt: time.Now().UTC().Add(-(presenceStatsStaleAfter + 60) * time.Second),
		online:     map[string]int64{bindingID: 5}, // stale table still claims sessions
	}
	if err := state.trafficCollector.ResetProviders([]client.TrafficProvider{provider}); err != nil {
		t.Fatal(err)
	}
	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("collect: %v", err)
	}

	item := presenceItemByClientID(t, presenceItemsByClientID(t, presenceRequest(t, state)), row.ID)
	if item.Online == nil || *item.Online {
		t.Fatalf("online=%v, want false — a stale table must not report online", item.Online)
	}
	if item.Source != presenceSourceActivity {
		t.Fatalf("source=%q, want activity fallback for a stale table", item.Source)
	}
	if item.Connections != nil {
		t.Fatalf("connections=%v, want absent when stats are stale", *item.Connections)
	}
}

// TestV1PresenceMieruActivityWindow: the counter-increase heuristic —
// fresh increase online, stale increase offline, never-sampled honest null.
func TestV1PresenceMieruActivityWindow(t *testing.T) {
	state := newClientLifecycleTestState(t)
	state.mu.Lock()
	state.inbounds = []Inbound{{Name: "mi-main", Protocol: "mieru", Enabled: true}}
	state.mu.Unlock()

	fresh := mustCreatePresenceClient(t, state, "mieru-fresh",
		client.Binding{InboundID: "mi-main", Enabled: true})
	stale := mustCreatePresenceClient(t, state, "mieru-stale",
		client.Binding{InboundID: "mi-main", Enabled: true})
	never := mustCreatePresenceClient(t, state, "mieru-never",
		client.Binding{InboundID: "mi-main", Enabled: true})

	bindings, err := state.clientRepo.AllBindings()
	if err != nil {
		t.Fatal(err)
	}
	bindingOf := map[string]string{}
	for _, b := range bindings {
		bindingOf[b.ClientID] = b.ID
	}
	now := time.Now().Unix()
	if err := state.trafficStore.RecordSample(client.Sample{
		BindingID: bindingOf[fresh.ID], UploadBytes: 512, AtUnix: now,
	}); err != nil {
		t.Fatalf("record fresh increase: %v", err)
	}
	staleAt := now - presenceActivityWindow - 60
	if err := state.trafficStore.RecordSample(client.Sample{
		BindingID: bindingOf[stale.ID], UploadBytes: 512, AtUnix: staleAt,
	}); err != nil {
		t.Fatalf("record stale increase: %v", err)
	}

	items := presenceItemsByClientID(t, presenceRequest(t, state))

	freshItem := presenceItemByClientID(t, items, fresh.ID)
	if freshItem.Online == nil || !*freshItem.Online {
		t.Fatalf("fresh increase online=%v, want true", freshItem.Online)
	}
	if freshItem.Source != presenceSourceActivity {
		t.Fatalf("fresh source=%q, want activity", freshItem.Source)
	}
	if freshItem.LastActiveAt == nil || *freshItem.LastActiveAt != now {
		t.Fatalf("fresh lastActiveAt=%v, want %d", freshItem.LastActiveAt, now)
	}

	staleItem := presenceItemByClientID(t, items, stale.ID)
	if staleItem.Online == nil || *staleItem.Online {
		t.Fatalf("stale increase online=%v, want false", staleItem.Online)
	}
	if staleItem.Source != presenceSourceActivity {
		t.Fatalf("stale source=%q, want activity", staleItem.Source)
	}
	if staleItem.LastActiveAt == nil || *staleItem.LastActiveAt != staleAt {
		t.Fatalf("stale lastActiveAt=%v, want %d (the true last increase)", staleItem.LastActiveAt, staleAt)
	}

	// Never sampled and no provider has ever reported: the honest answer is
	// null, not a fabricated offline.
	neverItem := presenceItemByClientID(t, items, never.ID)
	if neverItem.Online != nil {
		t.Fatalf("never-sampled online=%v, want null", *neverItem.Online)
	}
	if neverItem.Source != presenceSourceActivity {
		t.Fatalf("never-sampled source=%q, want activity (the mechanism that would answer)", neverItem.Source)
	}
	if neverItem.LastActiveAt != nil {
		t.Fatalf("never-sampled lastActiveAt=%v, want absent", *neverItem.LastActiveAt)
	}
}

// TestV1PresenceMieruHealthyProviderQuietIsOffline: once the mieru provider
// is observed-healthy, a binding whose counters have never increased is a
// proven "idle", not an unknown.
func TestV1PresenceMieruHealthyProviderQuietIsOffline(t *testing.T) {
	state := newClientLifecycleTestState(t)
	state.mu.Lock()
	state.inbounds = []Inbound{{Name: "mi-main", Protocol: "mieru", Enabled: true}}
	state.mu.Unlock()
	row := mustCreatePresenceClient(t, state, "mieru-quiet",
		client.Binding{InboundID: "mi-main", Enabled: true})

	provider := &presenceFakeProvider{key: mieruPresenceProviderKey}
	if err := state.trafficCollector.ResetProviders([]client.TrafficProvider{provider}); err != nil {
		t.Fatal(err)
	}
	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("collect: %v", err)
	}

	item := presenceItemByClientID(t, presenceItemsByClientID(t, presenceRequest(t, state)), row.ID)
	if item.Online == nil || *item.Online {
		t.Fatalf("online=%v, want false for a watched-but-quiet binding", item.Online)
	}
	if item.Source != presenceSourceActivity {
		t.Fatalf("source=%q, want activity", item.Source)
	}
}

// TestV1PresenceMultiBindingMerge: a client with several bindings is online
// when ANY binding is online; source is the most authoritative mechanism
// supporting the merged verdict and connections sum stats sources only.
func TestV1PresenceMultiBindingMerge(t *testing.T) {
	state := newClientLifecycleTestState(t)
	state.mu.Lock()
	state.inbounds = []Inbound{
		{Name: "hy-m", Protocol: "hysteria2", Enabled: true},
		{Name: "mi-m", Protocol: "mieru", Enabled: true},
	}
	state.mu.Unlock()

	// Client one: hy2 stats say offline (0 sessions) but mieru counters just
	// grew — merged verdict is online via activity, with the stats-reported
	// connection count (0) still surfaced.
	busy := mustCreatePresenceClient(t, state, "busy",
		client.Binding{InboundID: "hy-m", Enabled: true},
		client.Binding{InboundID: "mi-m", Enabled: true})
	// Client two: hy2 stats report 2 sessions while its mieru binding is
	// stale — merged verdict online via stats.
	statsOnly := mustCreatePresenceClient(t, state, "stats-only",
		client.Binding{InboundID: "hy-m", Enabled: true},
		client.Binding{InboundID: "mi-m", Enabled: true})
	// Client three: hy2 offline and mieru stale — proven offline, stats wins
	// the source slot because both mechanisms agree on false.
	idle := mustCreatePresenceClient(t, state, "idle",
		client.Binding{InboundID: "hy-m", Enabled: true},
		client.Binding{InboundID: "mi-m", Enabled: true})

	bindings, err := state.clientRepo.AllBindings()
	if err != nil {
		t.Fatal(err)
	}
	hyOnline := map[string]int64{}
	miBindingOf := map[string]string{}
	for _, b := range bindings {
		if b.InboundID == "hy-m" {
			switch b.ClientID {
			case statsOnly.ID:
				hyOnline[b.ID] = 2
			default:
				hyOnline[b.ID] = 0
			}
		} else {
			miBindingOf[b.ClientID] = b.ID
		}
	}
	provider := &presenceFakeProvider{key: "hysteria2:hy-m", online: hyOnline}
	if err := state.trafficCollector.ResetProviders([]client.TrafficProvider{provider}); err != nil {
		t.Fatal(err)
	}
	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("collect: %v", err)
	}

	now := time.Now().Unix()
	if err := state.trafficStore.RecordSample(client.Sample{BindingID: miBindingOf[busy.ID], UploadBytes: 64, AtUnix: now}); err != nil {
		t.Fatal(err)
	}
	if err := state.trafficStore.RecordSample(client.Sample{BindingID: miBindingOf[statsOnly.ID], UploadBytes: 64, AtUnix: now - presenceActivityWindow - 30}); err != nil {
		t.Fatal(err)
	}
	if err := state.trafficStore.RecordSample(client.Sample{BindingID: miBindingOf[idle.ID], UploadBytes: 64, AtUnix: now - presenceActivityWindow - 30}); err != nil {
		t.Fatal(err)
	}

	items := presenceItemsByClientID(t, presenceRequest(t, state))

	busyItem := presenceItemByClientID(t, items, busy.ID)
	if busyItem.Online == nil || !*busyItem.Online {
		t.Fatalf("busy online=%v, want true (any binding online wins)", busyItem.Online)
	}
	if busyItem.Source != presenceSourceActivity {
		t.Fatalf("busy source=%q, want activity — the mechanism that proved online", busyItem.Source)
	}
	if busyItem.Connections == nil || *busyItem.Connections != 0 {
		t.Fatalf("busy connections=%v, want 0 summed from stats bindings", busyItem.Connections)
	}

	statsItem := presenceItemByClientID(t, items, statsOnly.ID)
	if statsItem.Online == nil || !*statsItem.Online {
		t.Fatalf("statsOnly online=%v, want true", statsItem.Online)
	}
	if statsItem.Source != presenceSourceStats {
		t.Fatalf("statsOnly source=%q, want stats", statsItem.Source)
	}
	if statsItem.Connections == nil || *statsItem.Connections != 2 {
		t.Fatalf("statsOnly connections=%v, want 2", statsItem.Connections)
	}

	idleItem := presenceItemByClientID(t, items, idle.ID)
	if idleItem.Online == nil || *idleItem.Online {
		t.Fatalf("idle online=%v, want false — stats and stale activity agree", idleItem.Online)
	}
	if idleItem.Source != presenceSourceStats {
		t.Fatalf("idle source=%q, want stats as the most authoritative false", idleItem.Source)
	}
}

// TestV1PresenceWithoutCollector: no traffic collector and no store still
// answers 200 with honest items — capability-backed bindings read as
// unknown-activity rather than offline, unsupported stays unsupported.
func TestV1PresenceWithoutCollector(t *testing.T) {
	db := testdb.Open(t)
	defer db.Close()
	state := &managementState{
		clientRepo: client.NewRepository(db),
	}
	state.inbounds = []Inbound{
		{Name: "mi-main", Protocol: "mieru", Enabled: true},
		{Name: "naive-in", Protocol: "naiveproxy", Enabled: true},
	}
	mieruClient := mustCreatePresenceClient(t, state, "mieru",
		client.Binding{InboundID: "mi-main", Enabled: true})
	naiveClient := mustCreatePresenceClient(t, state, "naive",
		client.Binding{InboundID: "naive-in", Enabled: true})
	// Disabled bindings carry no telemetry and must not even claim the
	// activity mechanism.
	disabledClient := mustCreatePresenceClient(t, state, "disabled",
		client.Binding{InboundID: "mi-main", Enabled: false})

	items := presenceItemsByClientID(t, presenceRequest(t, state))

	mieruItem := presenceItemByClientID(t, items, mieruClient.ID)
	if mieruItem.Online != nil || mieruItem.Source != presenceSourceActivity {
		t.Fatalf("mieru item=%+v, want online=null source=activity (never sampled, provider dark)", mieruItem)
	}
	naiveItem := presenceItemByClientID(t, items, naiveClient.ID)
	if naiveItem.Online != nil || naiveItem.Source != presenceSourceUnsupported {
		t.Fatalf("naive item=%+v, want online=null source=unsupported", naiveItem)
	}
	disabledItem := presenceItemByClientID(t, items, disabledClient.ID)
	if disabledItem.Online != nil || disabledItem.Source != presenceSourceUnsupported {
		t.Fatalf("disabled-binding item=%+v, want online=null source=unsupported", disabledItem)
	}
}

// TestPresenceActivityEvalEdges locks the heuristic's corner cases at the
// unit level.
func TestPresenceActivityEvalEdges(t *testing.T) {
	now := time.Now().Unix()
	healthy := client.ProviderHealth{State: "healthy"}
	degraded := client.ProviderHealth{State: "degraded"}

	fresh := activityPresenceEval(now-10, healthy, now)
	if fresh.online == nil || !*fresh.online || fresh.source != presenceSourceActivity {
		t.Fatalf("fresh eval=%+v, want true/activity", fresh)
	}
	stale := activityPresenceEval(now-presenceActivityWindow-1, healthy, now)
	if stale.online == nil || *stale.online {
		t.Fatalf("stale eval=%+v, want false", stale)
	}
	quiet := activityPresenceEval(0, healthy, now)
	if quiet.online == nil || *quiet.online {
		t.Fatalf("watched-quiet eval=%+v, want false", quiet)
	}
	for _, dark := range []client.ProviderHealth{degraded, {}} {
		never := activityPresenceEval(0, dark, now)
		if never.online != nil {
			t.Fatalf("never-sampled eval with health %q = online %v, want null", dark.State, *never.online)
		}
	}
}

// TestPresenceMergeAllUnknownYieldsNull: bindings whose only mechanism is
// dark produce a null verdict attributed to the best mechanism that could
// have answered — never unsupported, never false.
func TestPresenceMergeAllUnknownYieldsNull(t *testing.T) {
	c := client.Client{ID: "c1", Name: "c1"}
	merged := mergeClientPresence(c, []bindingPresenceEval{
		{rank: presenceRankActivity, source: presenceSourceActivity},
		{rank: presenceRankUnsupported, source: presenceSourceUnsupported},
	})
	if merged.Online != nil {
		t.Fatalf("merged online=%v, want null", *merged.Online)
	}
	if merged.Source != presenceSourceActivity {
		t.Fatalf("merged source=%q, want activity (best mechanism that could have answered)", merged.Source)
	}
}

// TestV1PresenceIneligibleClientActivitySource (#1177): disabled, depleted,
// and expired clients are already excluded by the render path, so a fresh
// counter increase — residual telemetry — must not report them online via
// the activity heuristic. An enabled live client must still report online.
func TestV1PresenceIneligibleClientActivitySource(t *testing.T) {
	state := newClientLifecycleTestState(t)
	state.mu.Lock()
	state.inbounds = []Inbound{{Name: "mi-main", Protocol: "mieru", Enabled: true}}
	state.mu.Unlock()

	past := time.Now().Unix() - 3600
	ineligible := []client.Client{
		mustCreatePresenceClientRow(t, state,
			client.Client{Name: "disabled", Enabled: false, QuotaResetPolicy: client.ResetNever},
			client.Binding{InboundID: "mi-main", Enabled: true}),
		mustCreatePresenceClientRow(t, state,
			client.Client{Name: "depleted", Enabled: true, Depleted: true, QuotaResetPolicy: client.ResetNever},
			client.Binding{InboundID: "mi-main", Enabled: true}),
		mustCreatePresenceClientRow(t, state,
			client.Client{Name: "expired", Enabled: true, ExpiresAt: &past, QuotaResetPolicy: client.ResetNever},
			client.Binding{InboundID: "mi-main", Enabled: true}),
	}
	live := mustCreatePresenceClient(t, state, "live",
		client.Binding{InboundID: "mi-main", Enabled: true})

	// Fresh counter increases on every binding: residual telemetry for the
	// ineligible clients, a real sighting for the eligible one.
	bindings, err := state.clientRepo.AllBindings()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	for _, b := range bindings {
		if err := state.trafficStore.RecordSample(client.Sample{
			BindingID: b.ID, UploadBytes: 128, AtUnix: now,
		}); err != nil {
			t.Fatalf("record activity for binding %s: %v", b.ID, err)
		}
	}

	items := presenceItemsByClientID(t, presenceRequest(t, state))
	for _, c := range ineligible {
		item := presenceItemByClientID(t, items, c.ID)
		if item.Online != nil {
			t.Fatalf("client %q online=%v, want null — residual activity must not prove presence", c.Name, *item.Online)
		}
		if item.Source != presenceSourceIneligible {
			t.Fatalf("client %q source=%q, want ineligible", c.Name, item.Source)
		}
		if item.Connections != nil || item.LastActiveAt != nil {
			t.Fatalf("client %q carries residual telemetry fields: %+v", c.Name, item)
		}
	}

	liveItem := presenceItemByClientID(t, items, live.ID)
	if liveItem.Online == nil || !*liveItem.Online {
		t.Fatalf("live client online=%v, want true — the eligibility gate must not over-filter", liveItem.Online)
	}
	if liveItem.Source != presenceSourceActivity {
		t.Fatalf("live client source=%q, want activity", liveItem.Source)
	}
}

// TestV1PresenceIneligibleClientStatsSource (#1177): a residual hysteria2
// /online row still claiming live sessions must not report a disabled,
// depleted, or expired client online — the stats path obeys the same
// eligibility gate.
func TestV1PresenceIneligibleClientStatsSource(t *testing.T) {
	state := newClientLifecycleTestState(t)
	state.mu.Lock()
	state.inbounds = []Inbound{{Name: "hy-main", Protocol: "hysteria2", Enabled: true}}
	state.mu.Unlock()

	past := time.Now().Unix() - 3600
	ineligible := []client.Client{
		mustCreatePresenceClientRow(t, state,
			client.Client{Name: "disabled", Enabled: false, QuotaResetPolicy: client.ResetNever},
			client.Binding{InboundID: "hy-main", Enabled: true}),
		mustCreatePresenceClientRow(t, state,
			client.Client{Name: "depleted", Enabled: true, Depleted: true, QuotaResetPolicy: client.ResetNever},
			client.Binding{InboundID: "hy-main", Enabled: true}),
		mustCreatePresenceClientRow(t, state,
			client.Client{Name: "expired", Enabled: true, ExpiresAt: &past, QuotaResetPolicy: client.ResetNever},
			client.Binding{InboundID: "hy-main", Enabled: true}),
	}
	live := mustCreatePresenceClient(t, state, "live",
		client.Binding{InboundID: "hy-main", Enabled: true})

	bindings, err := state.clientRepo.AllBindings()
	if err != nil {
		t.Fatal(err)
	}
	online := map[string]int64{}
	for _, b := range bindings {
		online[b.ID] = 2 // residual session rows for all bindings
	}
	provider := &presenceFakeProvider{key: "hysteria2:hy-main", online: online}
	if err := state.trafficCollector.ResetProviders([]client.TrafficProvider{provider}); err != nil {
		t.Fatal(err)
	}
	if err := state.trafficCollector.CollectOnce(); err != nil {
		t.Fatalf("collect: %v", err)
	}

	items := presenceItemsByClientID(t, presenceRequest(t, state))

	for _, c := range ineligible {
		item := presenceItemByClientID(t, items, c.ID)
		if item.Online != nil {
			t.Fatalf("client %q online=%v, want null — a residual stats row must not prove presence", c.Name, *item.Online)
		}
		if item.Source != presenceSourceIneligible {
			t.Fatalf("client %q source=%q, want ineligible", c.Name, item.Source)
		}
		if item.Connections != nil || item.LastActiveAt != nil {
			t.Fatalf("client %q carries residual telemetry fields: %+v", c.Name, item)
		}
	}

	liveItem := presenceItemByClientID(t, items, live.ID)
	if liveItem.Online == nil || !*liveItem.Online {
		t.Fatalf("live client online=%v, want true from stats", liveItem.Online)
	}
	if liveItem.Source != presenceSourceStats {
		t.Fatalf("live client source=%q, want stats", liveItem.Source)
	}
	if liveItem.Connections == nil || *liveItem.Connections != 2 {
		t.Fatalf("live client connections=%v, want 2", liveItem.Connections)
	}
}

// TestOpenAPIPresenceItemMatchesGoWire locks the documented schema to the Go
// wire struct so the spec cannot silently drift from what the runtime emits.
func TestOpenAPIPresenceItemMatchesGoWire(t *testing.T) {
	schemas := loadOpenAPISchemas(t)
	assertSchemaMatchesGoWire(t, schemas, "PresenceItem", reflect.TypeOf(presenceItem{}))

	response, ok := schemas["PresenceResponse"]
	if !ok {
		t.Fatal("OpenAPI schema PresenceResponse not found")
	}
	for _, prop := range []string{"items", "count"} {
		if _, ok := response.Properties[prop]; !ok {
			t.Fatalf("PresenceResponse does not document %q", prop)
		}
	}
}
