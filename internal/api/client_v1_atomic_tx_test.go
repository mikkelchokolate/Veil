package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/client"
)

// Blocker-A1 tests: client state, desired revision, and the immutable snapshot
// commit atomically. A revision/snapshot failure fails the mutation and leaves
// NO committed client rows behind.

// TestClientMutationRollbackOnRevisionFailure breaks the revision machinery
// and asserts a client create fails honestly instead of committing a client
// with no pinned revision (the former log-and-continue behaviour).
func TestClientMutationRollbackOnRevisionFailure(t *testing.T) {
	r, st := newApplyTrackedRouterWithState(t)
	desired0, _ := applyState(t, r)

	// Sabotage the revision machinery: the revision bump inside the atomic
	// transaction will now fail.
	if _, err := st.db.Exec(`DROP TABLE revisions`); err != nil {
		t.Fatalf("sabotage revisions: %v", err)
	}

	w := postJSON(t, r, "/api/v1/clients", `{"name":"atomic-victim"}`)
	if w.Code == http.StatusCreated {
		t.Fatalf("create succeeded despite broken revision store: %s", w.Body.String())
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("create status = %d, want 500: %s", w.Code, w.Body.String())
	}

	// Nothing committed: the client must NOT exist.
	clients, total, err := st.clientRepo.List(client.ListFilter{})
	if err != nil {
		t.Fatalf("list clients: %v", err)
	}
	if total != 0 {
		t.Fatalf("client committed despite failed revision bump: %+v", clients)
	}
	_ = desired0 // revision store is broken; nothing more to assert against it
}

// TestClientMutationRollbackOnMutateFailure asserts a failure mid-mutation
// (second binding on a duplicate inbound) commits nothing: no client, no
// revision, no apply job.
func TestClientMutationRollbackOnMutateFailure(t *testing.T) {
	r, _ := newApplyTrackedRouterWithState(t)
	w := postJSON(t, r, "/api/inbounds", `{"name":"tx-inbound","protocol":"hysteria2","transport":"udp","port":14433,"enabled":true}`)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", w.Code, w.Body.String())
	}
	desired0, _ := applyState(t, r)
	jobs0 := len(listApplyJobs(t, r))

	// Duplicate binding to the same inbound -> unique violation mid-transaction.
	w = postJSON(t, r, "/api/v1/clients", `{"name":"tx-victim","bindings":[{"inboundId":"tx-inbound"},{"inboundId":"tx-inbound"}]}`)
	if w.Code == http.StatusCreated {
		t.Fatalf("create succeeded despite duplicate bindings: %s", w.Body.String())
	}

	// The whole mutation rolled back: client absent, revision unchanged, no
	// new apply job.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/clients", nil))
	if strings.Contains(w.Body.String(), "tx-victim") {
		t.Fatalf("client visible after rolled-back create: %s", w.Body.String())
	}
	desired1, _ := applyState(t, r)
	if desired1 != desired0 {
		t.Fatalf("desired revision moved on rolled-back mutation: %d -> %d", desired0, desired1)
	}
	if got := len(listApplyJobs(t, r)); got != jobs0 {
		t.Fatalf("apply jobs changed on rolled-back mutation: %d -> %d", jobs0, got)
	}
}

// TestBulkNoChangeCreatesNoRevision asserts a bulk where every client fails
// records NO revision, NO snapshot, and NO apply job — nothing changed, so the
// system must not pretend a new desired state exists.
func TestBulkNoChangeCreatesNoRevision(t *testing.T) {
	r, _ := newApplyTrackedRouterWithState(t)
	desired0, _ := applyState(t, r)
	jobs0 := len(listApplyJobs(t, r))

	w := postJSON(t, r, "/api/v1/clients/bulk", `{"action":"enable","clientIds":["does-not-exist"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("bulk: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode bulk: %v", err)
	}
	if resp.Succeeded != 0 || resp.Failed != 1 {
		t.Fatalf("bulk summary = %+v, want 0 succeeded / 1 failed", resp)
	}
	desired1, _ := applyState(t, r)
	if desired1 != desired0 {
		t.Fatalf("desired revision moved for a no-change bulk: %d -> %d", desired0, desired1)
	}
	if got := len(listApplyJobs(t, r)); got != jobs0 {
		t.Fatalf("apply job created for a no-change bulk: %d -> %d", jobs0, got)
	}
}

// TestTrafficReconcilerCreatesRevisionAndSnapshot (blocker A2): quota
// reconciliation routes through the unified mutation orchestration — the
// depleted flip commits atomically with a new desired revision, an immutable
// snapshot containing the depleted state, and exactly one apply job.
func TestTrafficReconcilerCreatesRevisionAndSnapshot(t *testing.T) {
	r, st := newApplyTrackedRouterWithState(t)
	w := postJSON(t, r, "/api/inbounds", `{"name":"quota-inbound","protocol":"hysteria2","transport":"udp","port":14434,"enabled":true}`)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", w.Code, w.Body.String())
	}
	w = postJSON(t, r, "/api/v1/clients", `{"name":"quota-victim","quotaBytes":1000,"bindings":[{"inboundId":"quota-inbound"}]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Client struct {
			ID       string `json:"id"`
			Bindings []struct {
				ID string `json:"id"`
			} `json:"bindings"`
		} `json:"client"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Client.ID == "" || len(created.Client.Bindings) == 0 {
		t.Fatalf("create response missing client/bindings: %s", w.Body.String())
	}

	// Record usage over the quota.
	if err := st.trafficStore.RecordSample(client.Sample{
		BindingID: created.Client.Bindings[0].ID, UploadBytes: 700, DownloadBytes: 500, AtUnix: 1,
	}); err != nil {
		t.Fatalf("record sample: %v", err)
	}

	desired0, _ := applyState(t, r)
	jobs0 := len(listApplyJobs(t, r))

	changed, err := st.trafficReconciler.ReconcileOnce()
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if changed != 1 {
		t.Fatalf("reconcile changed = %d, want 1", changed)
	}

	// Depleted flag flipped.
	c, err := st.clientRepo.Get(created.Client.ID)
	if err != nil {
		t.Fatalf("get client: %v", err)
	}
	if !c.Depleted {
		t.Fatal("client not marked depleted after reconcile")
	}

	// Exactly one new desired revision and exactly one new apply job.
	desired1, _ := applyState(t, r)
	if desired1 != desired0+1 {
		t.Fatalf("desired revision = %d, want %d after reconcile", desired1, desired0+1)
	}
	if got := len(listApplyJobs(t, r)); got != jobs0+1 {
		t.Fatalf("jobs after reconcile = %d, want %d", got, jobs0+1)
	}

	// The pinned snapshot for the new revision contains the depleted state.
	raw, err := st.applySnapshots.Load(desired1)
	if err != nil {
		t.Fatalf("load snapshot rev %d: %v", desired1, err)
	}
	var snap struct {
		Clients []struct {
			ID       string `json:"id"`
			Depleted bool   `json:"depleted"`
		} `json:"clients"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	found := false
	for _, sc := range snap.Clients {
		if sc.ID == created.Client.ID && sc.Depleted {
			found = true
		}
	}
	if !found {
		t.Fatalf("pinned snapshot for revision %d does not contain depleted client: %s", desired1, raw)
	}
}

// TestStartupMigrateLegacyMarkerBackupAndIdempotency (blocker A3): a normal
// boot (no SIGHUP) migrates legacy profiles with a pre-flight backup, records
// a migration marker, and later boots take the marker fast path without
// re-migrating or re-backing-up.
func TestStartupMigrateLegacyMarkerBackupAndIdempotency(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")
	info := ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, KeyPath: keyPath, ApplyRoot: filepath.Join(dir, "apply")}

	// Seed a state file with a legacy inbound carrying embedded profiles.
	if err := atomicfile.Write(statePath, []byte(`{"schemaVersion":4,"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"legacy.example.com"},"inbounds":[{"name":"hy2","protocol":"hysteria2","transport":"udp","port":443,"enabled":true,"profiles":[{"username":"alice","password":"alice-pass","enabled":true},{"username":"bob","password":"bob-pass","enabled":true}]}]}`), 0o600, 0o700); err != nil {
		t.Fatalf("write state: %v", err)
	}

	// Boot 1: normal startup path runs the migration.
	st1 := newManagementState(info)
	if st1.clientRepo == nil {
		t.Fatal("client repo not wired")
	}
	_, total, err := st1.clientRepo.List(client.ListFilter{})
	if err != nil {
		t.Fatalf("list clients: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 migrated clients after boot, got %d", total)
	}

	// Marker recorded with the expected version.
	marker, err := st1.clientRepo.GetMigrationMarker(legacyProfilesMarkerKey)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if marker == nil {
		t.Fatal("migration marker not recorded")
	}
	if marker.Version != legacyProfilesMarkerVersion {
		t.Fatalf("marker version = %d, want %d", marker.Version, legacyProfilesMarkerVersion)
	}

	// Backup exists with both the state file and the database copy.
	backups, err := filepath.Glob(filepath.Join(dir, "backups", "migrations", "legacy-profiles-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected exactly 1 migration backup dir, got %v (err %v)", backups, err)
	}
	for _, name := range []string{"state.json.bak", "veil.db.bak"} {
		if _, err := os.Stat(filepath.Join(backups[0], name)); err != nil {
			t.Fatalf("backup missing %s: %v", name, err)
		}
	}

	// Boot 2: marker fast path — no new backup, no duplicate clients.
	st2 := newManagementState(info)
	_, total, err = st2.clientRepo.List(client.ListFilter{})
	if err != nil {
		t.Fatalf("list clients after second boot: %v", err)
	}
	if total != 2 {
		t.Fatalf("second boot duplicated clients: got %d, want 2", total)
	}
	backups2, err := filepath.Glob(filepath.Join(dir, "backups", "migrations", "legacy-profiles-*"))
	if err != nil || len(backups2) != 1 {
		t.Fatalf("second boot created another backup: %v (err %v)", backups2, err)
	}
}
