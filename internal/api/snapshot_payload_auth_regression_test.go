package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// #1280: revision_snapshots.payload used to be returned verbatim. A veil.db
// writer could rewrite the applied row so /s/{token} rendered the victim's
// real ve1: credentials against attacker endpoints, or plant a desired row
// the privileged apply rendered. Every read is now authenticated by a MAC
// keyed from state.key.

const attackerDomain = "attacker-1280.example.net"

func rewriteSnapshotDomain(t *testing.T, state *managementState, revision uint64, blankMAC bool) {
	t.Helper()
	var payload string
	if err := state.db.QueryRow(`SELECT payload FROM revision_snapshots WHERE revision=?`, revision).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
		t.Fatal(err)
	}
	settings, _ := snapshot["settings"].(map[string]any)
	if settings == nil {
		settings = map[string]any{}
		snapshot["settings"] = settings
	}
	settings["domain"] = attackerDomain
	if inbounds, ok := snapshot["inbounds"].([]any); ok {
		for _, raw := range inbounds {
			if inbound, ok := raw.(map[string]any); ok {
				inbound["domain"] = attackerDomain
				inbound["sni"] = attackerDomain
			}
		}
	}
	rewritten, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	query := `UPDATE revision_snapshots SET payload=? WHERE revision=?`
	if blankMAC {
		query = `UPDATE revision_snapshots SET payload=?, payload_mac='' WHERE revision=?`
	}
	if _, err := state.db.Exec(query, string(rewritten), revision); err != nil {
		t.Fatal(err)
	}
}

func TestPublicFeedRefusesAppliedSnapshotRewrittenInDatabase(t *testing.T) {
	for _, blank := range []bool{false, true} {
		name := "edited sealed row"
		if blank {
			name = "edited row with blanked mac"
		}
		t.Run(name, func(t *testing.T) {
			router, state := newSubscriptionTestRouter(t)
			token, _ := seedClientWithToken(t, router)
			before := publicRawSubscription(t, router, token)
			if before.Code != http.StatusOK || !strings.Contains(before.Body.String(), "pw-alice") {
				t.Fatalf("honest feed: %d %q", before.Code, before.Body.String())
			}
			revisions, err := state.applyRevisions.Get()
			if err != nil {
				t.Fatal(err)
			}
			rewriteSnapshotDomain(t, state, revisions.Applied, blank)
			state.appliedProjectionMu.Lock()
			state.appliedProjections = nil
			state.appliedProjectionRevision = 0
			state.appliedProjectionMu.Unlock()

			after := publicRawSubscription(t, router, token)
			if strings.Contains(after.Body.String(), attackerDomain) {
				t.Fatalf("feed rendered a veil.db-authored endpoint: %q", after.Body.String())
			}
			if strings.Contains(after.Body.String(), "pw-alice") {
				t.Fatalf("feed still served real credentials from an unauthenticated snapshot: %d %q", after.Code, after.Body.String())
			}
		})
	}
}

func TestApplyRenderRefusesDesiredSnapshotRewrittenInDatabase(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	t.Cleanup(func() { _ = state.Close() })
	seedClientWithToken(t, router)
	revisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	rewriteSnapshotDomain(t, state, revisions.Desired, false)
	state.mu.Lock()
	restore, pinErr := state.pinStateToRevisionLocked(revisions.Desired)
	if restore != nil {
		restore()
	}
	_, loadErr := state.loadRevisionSnapshotLocked(revisions.Desired)
	state.mu.Unlock()
	if pinErr == nil {
		t.Fatal("pinned render accepted a rewritten desired snapshot")
	}
	if loadErr == nil {
		t.Fatal("revision snapshot load accepted a rewritten desired snapshot")
	}
}

func commitSettingsRevision(t *testing.T, router http.Handler, domain string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(
		`{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"`+domain+`"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("commit settings: %d %s", response.Code, response.Body.String())
	}
}

func restartPanel(t *testing.T, state *managementState) *managementState {
	t.Helper()
	statePath := state.statePath
	if err := closeClientSubsystem(state); err != nil {
		t.Fatalf("close panel database before restart: %v", err)
	}
	restarted := newManagementState(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: filepath.Dir(statePath)})
	t.Cleanup(func() { _ = closeClientSubsystem(restarted) })
	return restarted
}

func TestStartupSealsHonestLegacyDesiredSnapshot(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	seedClientWithToken(t, router)
	commitSettingsRevision(t, router, "legacy-1280.example.com")
	revisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a row written before migration v32: same payload, no MAC.
	if _, err := state.db.Exec(`UPDATE revision_snapshots SET payload_mac=''`); err != nil {
		t.Fatal(err)
	}
	restarted := restartPanel(t, state)
	if restarted.startupStateLoadFailed {
		t.Fatal("startup wedged on an honest pre-#1280 desired snapshot")
	}
	if _, err := restarted.applySnapshots.Load(revisions.Desired); err != nil {
		t.Fatalf("honest legacy desired row was not sealed: %v", err)
	}
	if revisions.Desired > 1 {
		if _, err := restarted.applySnapshots.Load(revisions.Desired - 1); err == nil {
			t.Fatal("an unverifiable pre-#1280 history row became readable")
		}
	}
}

func TestStartupRefusesTamperedSnapshotRows(t *testing.T) {
	cases := []struct {
		name  string
		blank bool
	}{
		{"sealed desired row edited", false},
		{"legacy-looking desired row edited after blanking mac", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, state := newApplyTrackedRouterWithState(t)
			commitSettingsRevision(t, router, "honest-1280.example.com")
			revisions, err := state.applyRevisions.Get()
			if err != nil {
				t.Fatal(err)
			}
			rewriteSnapshotDomain(t, state, revisions.Desired, tc.blank)
			restarted := restartPanel(t, state)
			if !restarted.startupStateLoadFailed {
				t.Fatal("startup accepted a veil.db-authored desired snapshot")
			}
			if restarted.settings.Domain == attackerDomain {
				t.Fatal("startup loaded the attacker domain")
			}
		})
	}
}

func TestStartupRefusesLegacyDesiredSnapshotWithPlaintextCredential(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)
	seedClientWithToken(t, router)
	revisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := state.db.QueryRow(`SELECT payload FROM revision_snapshots WHERE revision=?`, revisions.Desired).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var snapshot managementSnapshot
	if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Credentials) == 0 {
		t.Fatal("fixture has no credential")
	}
	snapshot.Credentials[0].EncryptedValue = []byte("attacker-chosen-password")
	rewritten, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.Exec(`UPDATE revision_snapshots SET payload=?, payload_mac='' WHERE revision=?`, string(rewritten), revisions.Desired); err != nil {
		t.Fatal(err)
	}
	restarted := restartPanel(t, state)
	if !restarted.startupStateLoadFailed {
		t.Fatal("startup sealed a legacy desired snapshot carrying a plaintext credential")
	}
}
