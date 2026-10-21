package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
)

// #1345 review: the legacy re-seal pass at startup used to be selected purely
// by payload_mac == "" on the desired row, a veil.db column. A veil.db writer
// could blank the MAC on the honestly sealed desired row, move credentials
// between clients (outside the state.json projection) and restart: the pass
// re-sealed the edited payload and alice's /s/ feed served bob's password.
// "Legacy sealing already happened" is now anchored in state.json.

func seedSecondClient(t *testing.T, router http.Handler, name, credential string) {
	t.Helper()
	w := v1Request(t, router, http.MethodPost, "/api/v1/clients", `{"name":"`+name+`","bindings":[{"inboundId":"hy2-sub","credential":"`+credential+`"}]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed %s: %d %s", name, w.Code, w.Body.String())
	}
	requireMutationEnvelopeSuccess(t, w.Body.Bytes(), "seed "+name)
}

// swapSnapshotCredentials swaps the first two credential ciphertexts in a
// revision row and blanks its MAC, leaving the state-file projection intact.
func swapSnapshotCredentials(t *testing.T, state *managementState, revision uint64) string {
	t.Helper()
	var payload string
	if err := state.db.QueryRow(`SELECT payload FROM revision_snapshots WHERE revision=?`, revision).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var snapshot managementSnapshot
	if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Credentials) < 2 {
		t.Fatalf("fixture has %d credentials, want 2", len(snapshot.Credentials))
	}
	snapshot.Credentials[0].EncryptedValue, snapshot.Credentials[1].EncryptedValue =
		snapshot.Credentials[1].EncryptedValue, snapshot.Credentials[0].EncryptedValue
	rewritten, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return string(rewritten)
}

func restartRouter(t *testing.T, state *managementState) (http.Handler, *managementState) {
	t.Helper()
	statePath := state.statePath
	if err := closeClientSubsystem(state); err != nil {
		t.Fatalf("close panel database before restart: %v", err)
	}
	router, reloader := newTestRouter(ServerInfo{Version: "test", Mode: "dev", StatePath: statePath, ApplyRoot: filepath.Dir(statePath)})
	restarted, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is not *managementState: %T", reloader)
	}
	t.Cleanup(func() { _ = closeClientSubsystem(restarted) })
	return router, restarted
}

func TestStartupRefusesBlankedMACOnSealedDesiredRowWithSwappedCredentials(t *testing.T) {
	cases := []struct {
		name  string
		plant func(*testing.T, *managementState, uint64, string)
	}{
		{"blank mac on the sealed desired row", func(t *testing.T, state *managementState, revision uint64, edited string) {
			if _, err := state.db.Exec(`UPDATE revision_snapshots SET payload=?, payload_mac='' WHERE revision=?`, edited, revision); err != nil {
				t.Fatal(err)
			}
		}},
		{"planted unsealed higher desired row", func(t *testing.T, state *managementState, revision uint64, edited string) {
			if _, err := state.db.Exec(`INSERT INTO revision_snapshots(revision, payload, state_sha256, created_at, payload_mac)
				SELECT revision+1, ?, state_sha256, created_at, '' FROM revision_snapshots WHERE revision=?`, edited, revision); err != nil {
				t.Fatal(err)
			}
			if _, err := state.db.Exec(`UPDATE revisions SET desired_revision=desired_revision+1 WHERE id=1`); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, state := newApplyTrackedRouterWithState(t)
			aliceToken, _ := seedClientWithToken(t, router)
			seedSecondClient(t, router, "bob", "pw-bob")
			before := publicRawSubscription(t, router, aliceToken)
			if before.Code != http.StatusOK || !strings.Contains(before.Body.String(), "pw-alice") || strings.Contains(before.Body.String(), "pw-bob") {
				t.Fatalf("honest feed: %d %q", before.Code, before.Body.String())
			}
			revisions, err := state.applyRevisions.Get()
			if err != nil {
				t.Fatal(err)
			}
			if revisions.Applied != revisions.Desired {
				t.Fatalf("fixture applied=%d desired=%d", revisions.Applied, revisions.Desired)
			}
			edited := swapSnapshotCredentials(t, state, revisions.Desired)
			tc.plant(t, state, revisions.Desired, edited)

			restartedRouter, restarted := restartRouter(t, state)
			if !restarted.startupStateLoadFailed {
				t.Fatal("startup re-sealed a veil.db-edited desired row whose MAC was blanked")
			}
			after := publicRawSubscription(t, restartedRouter, aliceToken)
			if strings.Contains(after.Body.String(), "pw-bob") {
				t.Fatalf("alice's feed served bob's credential: %d %q", after.Code, after.Body.String())
			}
			if restarted.applyRevisions != nil && restarted.applySnapshots != nil {
				current, err := restarted.applyRevisions.Get()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := restarted.applySnapshots.Load(current.Desired); err == nil {
					t.Fatal("the edited desired row became readable")
				}
			}
		})
	}
}

// #1345 re-review: an install whose desired revision is still 0 at the first
// start of a sealing build must also get the state.json anchor, or a row a
// veil.db writer plants later is sealed by the legacy pass.
func TestStartupAnchorsSnapshotSealingWithoutDesiredRevision(t *testing.T) {
	_, state := newApplyTrackedRouterWithState(t)
	revisions, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}
	if revisions.Desired != 0 {
		t.Fatalf("fixture desired=%d, want 0", revisions.Desired)
	}
	// The state file as a pre-#1280 build left it.
	preSeal := []byte(`{"schemaVersion":5,"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com"}}`)
	if err := atomicfile.Write(state.statePath, preSeal, 0o600, 0o700); err != nil {
		t.Fatal(err)
	}
	restarted := restartPanel(t, state)
	if restarted.startupStateLoadFailed {
		t.Fatal("startup failed on a desired-0 install")
	}
	body, err := os.ReadFile(restarted.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := managementstate.EncodedSchemaVersion(body); got < managementstate.SnapshotSealSchemaVersion {
		t.Fatalf("state.json after a desired-0 start is schema %d, want >= %d", got, managementstate.SnapshotSealSchemaVersion)
	}

	// A veil.db writer plants an unsealed desired row that would pass every
	// legacy check: its payload is the state file itself and it is bound to
	// the current digest.
	if _, err := restarted.db.Exec(`INSERT INTO revision_snapshots(revision, payload, state_sha256, created_at, payload_mac) VALUES(1, ?, ?, 1, '')`,
		string(body), managementstate.EncodedStateSHA256(body)); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.db.Exec(`UPDATE revisions SET desired_revision=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if again := restartPanel(t, restarted); !again.startupStateLoadFailed {
		t.Fatal("the legacy pass sealed a row planted after the first start of a desired-0 install")
	}
}
