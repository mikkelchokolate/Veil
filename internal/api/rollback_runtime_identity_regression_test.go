package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"testing"
)

func TestRollbackPreservesRuntimeIdentityAndProtocolConfigBytes(t *testing.T) {
	protocols := []struct {
		name      string
		protocol  string
		transport string
		port      int
	}{
		{name: "hysteria2", protocol: "hysteria2", transport: "udp", port: 16443},
		{name: "mieru", protocol: "mieru", transport: "tcp", port: 16444},
		{name: "naiveproxy", protocol: "naiveproxy", transport: "tcp", port: 443},
	}

	for _, protocol := range protocols {
		t.Run(protocol.name, func(t *testing.T) {
			router, state := newApplyTrackedRouterWithState(t)
			t.Cleanup(func() { _ = state.Close() })
			if protocol.protocol == "naiveproxy" {
				state.mu.Lock()
				state.settings.Email = "admin@example.com"
				state.settings.DefaultAcmeEmail = "admin@example.com"
				state.mu.Unlock()
			}
			inboundID := "rollback-" + protocol.name
			inboundBody := fmt.Sprintf(`{"name":%q,"protocol":%q,"transport":%q,"port":%d,"enabled":true}`,
				inboundID, protocol.protocol, protocol.transport, protocol.port)
			inbound := v1Request(t, router, http.MethodPost, "/api/inbounds", inboundBody)
			if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
				t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
			}

			const selectedIdentity = "custom_runtime_identity"
			createBody := fmt.Sprintf(`{"name":%q,"bindings":[{"inboundId":%q,"runtimeIdentity":%q,"credential":"rollback-secret"}]}`,
				"rollback-client-"+protocol.name, inboundID, selectedIdentity)
			createdResponse := v1Request(t, router, http.MethodPost, "/api/v1/clients", createBody)
			if createdResponse.Code != http.StatusCreated {
				t.Fatalf("create client: %d %s", createdResponse.Code, createdResponse.Body.String())
			}
			created := unwrapClient(t, createdResponse.Body.Bytes())
			clientID, _ := created["id"].(string)
			if clientID == "" {
				t.Fatalf("missing client ID: %s", createdResponse.Body.String())
			}
			selectedRevision, err := state.applyRevisions.Get()
			if err != nil {
				t.Fatal(err)
			}
			selectedLive := captureSnapshotTestTree(t, state.liveRoot)

			bindings, err := state.clientRepo.AllBindings()
			if err != nil || len(bindings) != 1 {
				t.Fatalf("bindings: count=%d err=%v", len(bindings), err)
			}
			binding := bindings[0]
			if binding.RuntimeIdentity != selectedIdentity {
				t.Fatalf("created runtime identity = %q, want %q", binding.RuntimeIdentity, selectedIdentity)
			}
			if _, err := state.db.Exec(`UPDATE client_bindings SET runtime_identity=?, version=version+1 WHERE id=?`, "newer_runtime_identity", binding.ID); err != nil {
				t.Fatalf("inject newer binding identity: %v", err)
			}
			currentClient, err := state.clientRepo.Get(clientID)
			if err != nil {
				t.Fatal(err)
			}
			update := v1Request(t, router, http.MethodPatch, "/api/v1/clients/"+clientID,
				fmt.Sprintf(`{"version":%d,"notes":"force newer immutable snapshot"}`, currentClient.Version))
			if update.Code != http.StatusOK {
				t.Fatalf("create newer revision: %d %s", update.Code, update.Body.String())
			}
			newerRevision, err := state.applyRevisions.Get()
			if err != nil {
				t.Fatal(err)
			}
			newerPayload, err := state.applySnapshots.Load(newerRevision.Desired)
			if err != nil {
				t.Fatal(err)
			}
			var newerSnapshot managementSnapshot
			if err := json.Unmarshal(newerPayload, &newerSnapshot); err != nil {
				t.Fatal(err)
			}
			if len(newerSnapshot.Bindings) != 1 {
				t.Fatalf("newer snapshot bindings = %d, want 1", len(newerSnapshot.Bindings))
			}
			if got := newerSnapshot.Bindings[0].RuntimeIdentity; got != "newer_runtime_identity" {
				t.Fatalf("newer snapshot RuntimeIdentity = %q, want injected identity", got)
			}
			newerLive := captureSnapshotTestTree(t, state.liveRoot)
			if reflect.DeepEqual(newerLive, selectedLive) {
				t.Fatal("changing RuntimeIdentity did not change the rendered protocol configuration")
			}

			rollback := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
				`{"selectedRevision":`+strconv.FormatUint(selectedRevision.Desired, 10)+`,"confirm":true}`)
			if rollback.Code != http.StatusOK {
				t.Fatalf("rollback: %d %s", rollback.Code, rollback.Body.String())
			}
			restoredBindings, err := state.clientRepo.AllBindings()
			if err != nil || len(restoredBindings) != 1 {
				t.Fatalf("restored bindings: count=%d err=%v", len(restoredBindings), err)
			}
			if got := restoredBindings[0].RuntimeIdentity; got != selectedIdentity {
				t.Errorf("rollback RuntimeIdentity = %q, want byte-identical %q", got, selectedIdentity)
			}
			restoredLive := captureSnapshotTestTree(t, state.liveRoot)
			if !reflect.DeepEqual(restoredLive, selectedLive) {
				t.Errorf("%s live protocol configuration was not reproduced byte-identically", protocol.name)
			}
		})
	}
}

func TestRollbackRejectsInvalidNormalizedSnapshotOwnershipAndIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*managementSnapshot)
	}{
		{
			name: "empty-runtime-identity",
			mutate: func(snapshot *managementSnapshot) {
				snapshot.Bindings[0].RuntimeIdentity = ""
			},
		},
		{
			name: "invalid-runtime-identity",
			mutate: func(snapshot *managementSnapshot) {
				snapshot.Bindings[0].RuntimeIdentity = "invalid identity!"
			},
		},
		{
			name: "duplicate-runtime-identity-within-inbound",
			mutate: func(snapshot *managementSnapshot) {
				duplicate := snapshot.Bindings[0]
				duplicate.ID = "duplicate-binding-id"
				snapshot.Bindings = append(snapshot.Bindings, duplicate)
			},
		},
		{
			name: "binding-missing-client-owner",
			mutate: func(snapshot *managementSnapshot) {
				snapshot.Bindings[0].ClientID = "missing-client-id"
			},
		},
		{
			name: "credential-missing-binding-owner",
			mutate: func(snapshot *managementSnapshot) {
				credential := snapshot.Credentials[0]
				credential.ID = "orphaned-credential-id"
				credential.BindingID = "missing-binding-id"
				snapshot.Credentials = append(snapshot.Credentials, credential)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router, state := newApplyTrackedRouterWithState(t)
			t.Cleanup(func() { _ = state.Close() })
			inbound := v1Request(t, router, http.MethodPost, "/api/inbounds",
				`{"name":"snapshot-owner-inbound","protocol":"hysteria2","transport":"udp","port":17443,"enabled":true}`)
			if inbound.Code != http.StatusCreated && inbound.Code != http.StatusOK {
				t.Fatalf("create inbound: %d %s", inbound.Code, inbound.Body.String())
			}
			createdResponse := v1Request(t, router, http.MethodPost, "/api/v1/clients",
				`{"name":"snapshot-owner","bindings":[{"inboundId":"snapshot-owner-inbound","runtimeIdentity":"valid_identity","credential":"secret"}]}`)
			if createdResponse.Code != http.StatusCreated {
				t.Fatalf("create client: %d %s", createdResponse.Code, createdResponse.Body.String())
			}
			created := unwrapClient(t, createdResponse.Body.Bytes())
			clientID := created["id"].(string)
			selected, err := state.applyRevisions.Get()
			if err != nil {
				t.Fatal(err)
			}
			payload, err := state.applySnapshots.Load(selected.Desired)
			if err != nil {
				t.Fatal(err)
			}
			var snapshot managementSnapshot
			if err := json.Unmarshal(payload, &snapshot); err != nil {
				t.Fatal(err)
			}
			test.mutate(&snapshot)
			corruptPayload, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := state.db.Exec(`UPDATE revision_snapshots SET payload=? WHERE revision=?`, string(corruptPayload), selected.Desired); err != nil {
				t.Fatal(err)
			}

			currentClient, err := state.clientRepo.Get(clientID)
			if err != nil {
				t.Fatal(err)
			}
			newer := v1Request(t, router, http.MethodPatch, "/api/v1/clients/"+clientID,
				fmt.Sprintf(`{"version":%d,"notes":"newer"}`, currentClient.Version))
			if newer.Code != http.StatusOK {
				t.Fatalf("create newer revision: %d %s", newer.Code, newer.Body.String())
			}
			beforeRevision, _ := state.applyRevisions.Get()
			stateBefore, err := os.ReadFile(state.statePath)
			if err != nil {
				t.Fatal(err)
			}

			rollback := v1Request(t, router, http.MethodPost, "/api/apply/rollback",
				`{"selectedRevision":`+strconv.FormatUint(selected.Desired, 10)+`,"confirm":true}`)
			if rollback.Code != http.StatusUnprocessableEntity {
				t.Errorf("invalid snapshot rollback status = %d, want 422: %s", rollback.Code, rollback.Body.String())
			}
			afterRevision, _ := state.applyRevisions.Get()
			if afterRevision != beforeRevision {
				t.Errorf("invalid snapshot advanced revisions: before=%+v after=%+v", beforeRevision, afterRevision)
			}
			stateAfter, err := os.ReadFile(state.statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stateAfter, stateBefore) {
				t.Error("invalid snapshot changed state.json")
			}
			var rollbackRows int
			if err := state.db.QueryRow(`SELECT COUNT(*) FROM apply_rollbacks`).Scan(&rollbackRows); err != nil {
				t.Fatal(err)
			}
			if rollbackRows != 0 {
				t.Errorf("invalid snapshot inserted %d rollback audit rows", rollbackRows)
			}
		})
	}
}
